package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/analeis/highjack/server/internal/game"
	"github.com/analeis/highjack/server/internal/logging"
	"github.com/analeis/highjack/server/internal/match"
)

// maxJoinBodyBytes bounds the join request body.
const maxJoinBodyBytes = 4 << 10

// MountMatches wires the private-match HTTP surface: create a match, join
// it, and read its authoritative snapshot. Gameplay itself happens only
// over the WebSocket seam.
func (s *Server) MountMatches(registry *match.Registry) {
	s.registry = registry
	s.router.HandleFunc("POST /matches", s.handleCreateMatch)
	s.router.HandleFunc("POST /matches/{matchId}/players", s.handleJoinMatch)
	s.router.HandleFunc("GET /matches/{matchId}", s.handleGetMatch)
}

type createMatchRequest struct {
	// Optional config override; omitted uses the default board ruleset.
	Config json.RawMessage `json:"config"`
}

type createMatchResponse struct {
	MatchID    string `json:"matchId"`
	ConfigHash string `json:"configHash"`
}

// handleCreateMatch mints a private match with an unguessable id.
func (s *Server) handleCreateMatch(w http.ResponseWriter, r *http.Request) {
	var req createMatchRequest
	// The body is read whenever the request carries one, not only when it
	// declares a length. A chunked request declares no length — `ContentLength`
	// is -1 — so gating on `> 0` silently ignored the config of every streaming
	// client and created the default board while the caller believed its own
	// ruleset had been accepted. A zero length still means "no body", which is
	// the only case worth skipping.
	if r.ContentLength != 0 {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, response{Status: "invalid_body"})
			return
		}
	}
	cfg := game.DefaultConfig()
	if len(req.Config) > 0 {
		parsed, err := game.ParseGameConfig(req.Config)
		if err != nil {
			logging.FromContext(r.Context()).Warn("match config rejected", "error", err.Error())
			writeError(w, http.StatusUnprocessableEntity, response{Status: "invalid_config"})
			return
		}
		cfg = *parsed
	}
	m, err := s.registry.Create(r.Context(), cfg)
	if err != nil {
		switch {
		case errors.Is(err, match.ErrRegistryFull):
			// The registry is at its bound. That is a capacity condition, not a
			// server fault: reporting it as 500 told an operator reading the logs
			// that the process was broken, when in fact it was working correctly
			// and refusing to accept more work. 503 is the honest answer, and it
			// is the status a load balancer can act on.
			logging.FromContext(r.Context()).Warn("match registry is full", "limit", match.MaxLiveMatches)
			writeError(w, http.StatusServiceUnavailable, response{Status: "registry_full"})
		default:
			logging.FromContext(r.Context()).Error("match creation failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, response{Status: "match_create_failed"})
		}
		return
	}
	hash, err := m.Config().Hash()
	if err != nil {
		writeError(w, http.StatusInternalServerError, response{Status: "hash_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, createMatchResponse{
		MatchID: string(m.ID()), ConfigHash: hash,
	})
}

type joinMatchRequest struct {
	DisplayName string `json:"displayName"`
}

type joinMatchResponse struct {
	PlayerID string `json:"playerId"`
	// Reconnect token: returned exactly once, never logged, never stored in
	// clear text. The server keeps only its sha256.
	Token   string `json:"token"`
	MatchID string `json:"matchId"`
	Seat    int    `json:"seat"`
}

// handleJoinMatch mints a seat + reconnect token in a private match.
func (s *Server) handleJoinMatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("matchId")
	m := s.registry.Get(game.GameID(id))
	if m == nil {
		writeError(w, http.StatusNotFound, response{Status: "match_not_found"})
		return
	}
	var req joinMatchRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJoinBodyBytes))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, response{Status: "invalid_body"})
		return
	}
	playerID, token, err := m.Join(r.Context(), req.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, game.ErrAlreadyStarted):
			writeError(w, http.StatusConflict, response{Status: "already_started"})
		case errors.Is(err, game.ErrGameFull):
			writeError(w, http.StatusConflict, response{Status: "game_full"})
		case errors.Is(err, game.ErrInvalidName):
			writeError(w, http.StatusUnprocessableEntity, response{Status: "invalid_name"})
		default:
			logging.FromContext(r.Context()).Error("join failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, response{Status: "join_failed"})
		}
		return
	}
	seat := 0
	if snap := m.Snapshot(); len(snap.Players) > 0 {
		for _, p := range snap.Players {
			if p.PlayerID == string(playerID) {
				seat = p.Seat
			}
		}
	}
	logging.FromContext(r.Context()).Info("player joined", "match", id, "player", playerID, "seat", seat)
	writeJSON(w, http.StatusCreated, joinMatchResponse{
		PlayerID: string(playerID), Token: token, MatchID: id, Seat: seat,
	})
}

// handleGetMatch returns the authoritative snapshot (lobby join screen).
func (s *Server) handleGetMatch(w http.ResponseWriter, r *http.Request) {
	m := s.registry.Get(game.GameID(r.PathValue("matchId")))
	if m == nil {
		writeError(w, http.StatusNotFound, response{Status: "match_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, m.Snapshot())
}
