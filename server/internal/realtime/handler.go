// Package realtime establishes the WebSocket transport seam.
//
//	WebSocket → Connection → Message Decoder → Protocol → (future) Game/Lobby Handler
//
// v0.1 scope is deliberately narrow and honest:
//
//   - GET /ws performs the protocol handshake (hello → welcome).
//   - ping/pong keepalive verification.
//   - game actions receive a structured `not_supported` error.
//
// This endpoint does not implement multiplayer gameplay. It exists so the
// transport, decoder, and error paths are real, tested infrastructure that
// the future lobby/game handlers plug into without rework.
package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/analeis/highjack/server/internal/protocol"

	"github.com/coder/websocket"
)

// Handler serves the /ws endpoint. All fields are read-only after
// construction; per-connection state lives in serveWS locals.
type Handler struct {
	log         *slog.Logger
	heartbeatMs int
}

func NewHandler(log *slog.Logger, heartbeatMs int) *Handler {
	return &Handler{log: log, heartbeatMs: heartbeatMs}
}

// Register mounts the WebSocket route on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws", h.serveWS)
}

const (
	handshakeReadTimeout = 10 * time.Second
	ioTimeout            = 5 * time.Second
	readIdleTimeout      = 90 * time.Second // clients must ping within this window
)

func (h *Handler) serveWS(w http.ResponseWriter, r *http.Request) {
	// Long-lived connection: clear the server's per-request deadlines for
	// this hijacked connection so WriteTimeout cannot kill the stream.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // tightened at the edge/proxy in production; see docs/architecture/BACKEND.md
	})
	if err != nil {
		h.log.Warn("websocket accept failed", "error", err.Error())
		return
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	session := newSessionID()

	if !h.handshake(conn, session) {
		return
	}
	h.log.Info("realtime handshake complete", "session", session)

	for {
		data, ok := h.readFrame(conn)
		if !ok {
			return
		}
		kind, _, ping, action, perr := protocol.DecodeClientMessage(data)
		if perr != nil {
			if !h.sendError(conn, nil, perr.Code, perr.Msg) {
				return
			}
			continue
		}
		switch kind {
		case protocol.KindPing:
			if !h.writeJSON(conn, map[string]any{
				"v":     protocol.ProtocolMajorVersion,
				"type":  "pong",
				"nonce": ping.Nonce,
			}) {
				return
			}
		case protocol.KindAction:
			if !h.sendErrorWithAck(conn, action.Seq, protocol.CodeNotSupported,
				"gameplay over WebSockets is not implemented yet") {
				return
			}
		case protocol.KindHello:
			// Tolerated in v0.1; a future connection state machine will reject it.
		default:
			if !h.sendError(conn, nil, protocol.CodeUnknownMessageType, "unhandled message kind") {
				return
			}
		}
	}
}

// handshake enforces hello-first semantics under a strict deadline.
func (h *Handler) handshake(conn *websocket.Conn, session string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), handshakeReadTimeout)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		h.sendError(conn, nil, protocol.CodeMalformedMessage, "failed to read handshake frame")
		return false
	}
	kind, hello, _, _, perr := protocol.DecodeClientMessage(data)
	if perr != nil {
		h.sendError(conn, nil, perr.Code, perr.Msg)
		return false
	}
	if kind != protocol.KindHello || hello == nil {
		h.sendError(conn, nil, protocol.CodeMalformedMessage, "first message must be a hello")
		return false
	}
	return h.writeJSON(conn, map[string]any{
		"v":               protocol.ProtocolMajorVersion,
		"type":            "welcome",
		"session":         session,
		"protocolVersion": protocol.ProtocolVersion,
		"heartbeatMs":     h.heartbeatMs,
	})
}

func (h *Handler) readFrame(conn *websocket.Conn) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), readIdleTimeout)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, false // timeout, close, or transport garbage
	}
	return data, true
}

func (h *Handler) writeJSON(conn *websocket.Conn, v any) bool {
	ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
	defer cancel()
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return conn.Write(ctx, websocket.MessageText, data) == nil
}

func (h *Handler) sendError(conn *websocket.Conn, ackSeq any, code protocol.ErrorCode, msg string) bool {
	payload := map[string]any{
		"v":    protocol.ProtocolMajorVersion,
		"type": "error",
		"error": map[string]any{
			"code":    string(code),
			"message": msg,
		},
	}
	if ackSeq != nil {
		payload["ackSeq"] = ackSeq
	}
	return h.writeJSON(conn, payload)
}

func (h *Handler) sendErrorWithAck(conn *websocket.Conn, ackSeq int64, code protocol.ErrorCode, msg string) bool {
	return h.sendError(conn, ackSeq, code, msg)
}

func newSessionID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "sess_fallback"
	}
	return "sess_" + hex.EncodeToString(buf[:])
}
