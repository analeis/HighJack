// Package realtime is the WebSocket transport: connection lifecycle,
// handshake, protocol decoding, and authoritative dispatch into the match
// runtime.
//
//	WebSocket → Connection → Message Decoder → Protocol → Match Runtime
//
// The handler owns no game rules and no state; it resolves the actor from
// the session binding, asks the match runtime to apply the action, and
// publishes whatever the engine accepted.
package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/analeis/highjack/server/internal/game"
	"github.com/analeis/highjack/server/internal/match"
	"github.com/analeis/highjack/server/internal/protocol"

	"github.com/coder/websocket"
)

const (
	handshakeReadTimeout = 10 * time.Second
	ioTimeout            = 5 * time.Second
	readIdleTimeout      = 90 * time.Second // clients must ping within this window
	// maxFrameBytes bounds a single inbound frame (v0.2 decision: explicit
	// 64 KiB limit; oversize frames close the connection).
	maxFrameBytes = 64 * 1024
	// publishTimeout bounds one event write so a slow consumer is dropped
	// rather than stalling the match.
	publishTimeout = 3 * time.Second
)

// Handler serves the /ws endpoint. Fields are read-only after construction.
type Handler struct {
	log         *slog.Logger
	heartbeatMs int
	registry    *match.Registry
	limiter     *match.RateLimiter
}

func NewHandler(log *slog.Logger, heartbeatMs int, registry *match.Registry) *Handler {
	// 20 actions per 10s sliding window per session (v0.2 decision).
	return &Handler{
		log:         log,
		heartbeatMs: heartbeatMs,
		registry:    registry,
		limiter:     match.NewRateLimiter(20, 10*time.Second),
	}
}

// Register mounts the WebSocket route on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws", h.serveWS)
}

func (h *Handler) serveWS(w http.ResponseWriter, r *http.Request) {
	// Long-lived connection: clear the server's per-request deadlines for
	// this hijacked connection so WriteTimeout cannot kill the stream.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  []string{"*"}, // tightened at the edge/proxy in production; see docs/architecture/BACKEND.md
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		h.log.Warn("websocket accept failed", "error", err.Error())
		return
	}
	conn.SetReadLimit(maxFrameBytes)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	c := &connSink{conn: conn, log: h.log}
	session := newSessionID()

	bound, ok := h.handshake(conn, c, session)
	if !ok {
		return
	}
	if bound != nil {
		defer func() { bound.Disconnect(session) }()
	}

	for {
		data, readOK := h.readFrame(conn)
		if !readOK {
			return
		}
		kind, _, ping, action, perr := protocol.DecodeClientMessage(data)
		if perr != nil {
			if !c.sendError(nil, perr.Code, perr.Msg) {
				return
			}
			continue
		}
		switch kind {
		case protocol.KindPing:
			if !c.sendPong(ping.Nonce) {
				return
			}
		case protocol.KindAction:
			if !h.handleAction(conn, c, session, bound, action) {
				return
			}
		case protocol.KindHello:
			if !c.sendError(nil, protocol.CodeMalformedMessage, "duplicate hello") {
				return
			}
		default:
			if !c.sendError(nil, protocol.CodeUnknownMessageType, "unhandled message kind") {
				return
			}
		}
	}
}

// handleAction decodes, dispatches, and publishes one gameplay action.
func (h *Handler) handleAction(conn *websocket.Conn, c *connSink, session string, m *match.Match, a *protocol.Action) bool {
	if m == nil {
		return c.sendErrorWithAck(a.Seq, protocol.CodeNotPermitted, "connection is not bound to a match")
	}
	if !h.limiter.Allow(session) {
		return c.sendErrorWithAck(a.Seq, protocol.CodeRateLimited, "too many actions; slow down")
	}
	action, perr := protocol.DecodeGameAction(a.Payload)
	if perr != nil {
		return c.sendErrorWithAck(a.Seq, perr.Code, perr.Msg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
	defer cancel()
	result, err := m.ApplyAction(ctx, session, a.Seq, action)
	if err != nil {
		// Distinguish domain rejections (recoverable) from durability
		// failures (do not ack success, do not publish).
		switch {
		case errors.Is(err, match.ErrUnboundSession):
			return c.sendErrorWithAck(a.Seq, protocol.CodeNotPermitted, "session is not bound to a player")
		case errors.Is(err, match.ErrStaleSequence):
			return c.sendErrorWithAck(a.Seq, protocol.CodeInvalidAction, "stale action sequence")
		default:
			h.log.Error("action not durable", "match", m.ID(), "seq", a.Seq, "error", err.Error())
			return c.sendErrorWithAck(a.Seq, protocol.CodeInternalError, "transition could not be persisted")
		}
	}
	if result.Err != nil {
		return c.sendErrorWithAck(a.Seq, result.DomainCode, result.Err.Error())
	}
	// Publish authoritative events to every bound connection in the match.
	m.Broadcast(result.Events, a.Seq)
	return c.sendAck(a.Seq, result.NextSeq)
}

// handshake performs hello→welcome, optional match binding, and sends the
// authoritative snapshot (+ catch-up when a cursor was supplied).
func (h *Handler) handshake(conn *websocket.Conn, c *connSink, session string) (*match.Match, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), handshakeReadTimeout)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		c.sendError(nil, protocol.CodeMalformedMessage, "failed to read handshake frame")
		return nil, false
	}
	kind, hello, _, _, perr := protocol.DecodeClientMessage(data)
	if perr != nil {
		c.sendError(nil, perr.Code, perr.Msg)
		return nil, false
	}
	if kind != protocol.KindHello || hello == nil {
		c.sendError(nil, protocol.CodeMalformedMessage, "first message must be a hello")
		return nil, false
	}

	var bound *match.Match
	if hello.MatchID != "" {
		m := h.registry.Get(game.GameID(hello.MatchID))
		if m == nil {
			c.sendError(nil, protocol.CodeNotPermitted, "unknown match")
			return nil, false
		}
		if _, err := m.Bind(session, hello.Token, hello.Client, c); err != nil {
			c.sendError(nil, protocol.CodeNotPermitted, "invalid join token")
			return nil, false
		}
		bound = m
	}

	welcome := map[string]any{
		"v": protocol.ProtocolMajorVersion, "type": "welcome", "session": session,
		"protocolVersion": protocol.ProtocolVersion, "heartbeatMs": h.heartbeatMs,
	}
	if bound != nil {
		welcome["matchId"] = string(bound.ID())
	}
	if !c.send(welcome) {
		return nil, false
	}
	if bound == nil {
		return nil, true
	}

	// Authoritative snapshot is always sent; catch-up is best-effort and
	// never replaces the snapshot.
	snap := bound.Snapshot()
	resync := false
	if hello.ResumeFromTick != nil {
		cursor := uint64(*hello.ResumeFromTick)
		if events, ok := bound.Since(cursor); ok && len(events) > 0 {
			c.sendCatchup(events, snap.Tick)
		} else if cursor > 0 && cursor < snap.Tick {
			// Cursor too old or unverified: tell the client to resync from
			// the snapshot rather than pretend catch-up succeeded.
			resync = true
		}
	}
	c.sendSnapshot(snap, bound.Config(), 1, resync)
	return bound, true
}

func (h *Handler) readFrame(conn *websocket.Conn) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), readIdleTimeout)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, false
	}
	return data, true
}

// ---- connection sink -------------------------------------------------------

// connSink implements match.Sink for a live WebSocket connection.
type connSink struct {
	conn *websocket.Conn
	log  *slog.Logger
	// failed latches a dead connection so the match stops writing to it.
	failed atomic.Bool
}

func (c *connSink) Send(msg any) bool {
	if c.failed.Load() {
		return false
	}
	if !c.send(msg) {
		c.failed.Store(true)
		return false
	}
	return true
}

func (c *connSink) send(v any) bool {
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return c.conn.Write(ctx, websocket.MessageText, data) == nil
}

func (c *connSink) sendPong(nonce int64) bool {
	return c.send(map[string]any{"v": protocol.ProtocolMajorVersion, "type": "pong", "nonce": nonce})
}

func (c *connSink) sendError(ackSeq any, code protocol.ErrorCode, msg string) bool {
	payload := map[string]any{
		"v": protocol.ProtocolMajorVersion, "type": "error",
		"error": map[string]any{"code": string(code), "message": msg},
	}
	if ackSeq != nil {
		payload["ackSeq"] = ackSeq
	}
	return c.send(payload)
}

func (c *connSink) sendErrorWithAck(seq int64, code protocol.ErrorCode, msg string) bool {
	return c.sendError(seq, code, msg)
}

func (c *connSink) sendAck(seq, nextSeq int64) bool {
	return c.send(map[string]any{
		"v": protocol.ProtocolMajorVersion, "type": "ack", "ackSeq": seq, "nextSeq": nextSeq,
	})
}

func (c *connSink) sendSnapshot(snap protocol.GameSnapshot, cfg *game.GameConfig, nextSeq int64, resync bool) {
	c.send(map[string]any{
		"v": protocol.ProtocolMajorVersion, "type": "snapshot",
		"snapshot": snap, "config": cfg, "nextSeq": nextSeq, "resync": resync,
	})
}

func (c *connSink) sendCatchup(events []match.EventEnvelope, cursor uint64) {
	type entry struct {
		Tick  uint64 `json:"tick"`
		Event any    `json:"event"`
	}
	out := make([]entry, 0, len(events))
	for _, e := range events {
		payload, err := protocol.EncodeEvent(e.Event)
		if err != nil {
			continue
		}
		var generic any
		if err := json.Unmarshal(payload, &generic); err != nil {
			continue
		}
		out = append(out, entry{Tick: e.Tick, Event: generic})
	}
	c.send(map[string]any{
		"v": protocol.ProtocolMajorVersion, "type": "catchup", "events": out, "cursor": cursor,
	})
}

func newSessionID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "sess_fallback"
	}
	return "sess_" + hex.EncodeToString(buf)
}
