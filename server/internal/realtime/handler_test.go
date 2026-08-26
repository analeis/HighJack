package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/protocol"

	"github.com/coder/websocket"
)

func wsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	h := NewHandler(log, 30000)
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, url string) (*websocket.Conn, *http.Response) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL(url), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn, resp
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func send(t *testing.T, conn *websocket.Conn, payload any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func receive(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("read non-JSON frame: %v", err)
	}
	return msg
}

func TestHandshakeHelloWelcome(t *testing.T) {
	srv := wsTestServer(t)
	conn, _ := dial(t, srv.URL+"/ws")

	send(t, conn, map[string]any{"v": 1, "type": "hello", "client": "test/1.0"})
	msg := receive(t, conn)

	if msg["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", msg)
	}
	if msg["protocolVersion"] != protocol.ProtocolVersion {
		t.Fatalf("protocolVersion = %v", msg["protocolVersion"])
	}
	session, _ := msg["session"].(string)
	if !strings.HasPrefix(session, "sess_") {
		t.Fatalf("session = %q", session)
	}
	if int(msg["heartbeatMs"].(float64)) != 30000 {
		t.Fatalf("heartbeatMs = %v", msg["heartbeatMs"])
	}
}

func TestHandshakeRejectsNonHelloFirstMessage(t *testing.T) {
	srv := wsTestServer(t)
	conn, _ := dial(t, srv.URL+"/ws")

	send(t, conn, map[string]any{"v": 1, "type": "ping", "nonce": 1})
	msg := receive(t, conn)

	if msg["type"] != "error" {
		t.Fatalf("expected error, got %v", msg)
	}
	errBody := msg["error"].(map[string]any)
	if errBody["code"] != string(protocol.CodeMalformedMessage) {
		t.Fatalf("code = %v", errBody["code"])
	}
}

func TestHandshakeRejectsWrongProtocolVersion(t *testing.T) {
	srv := wsTestServer(t)
	conn, _ := dial(t, srv.URL+"/ws")

	send(t, conn, map[string]any{"v": 99, "type": "hello", "client": "test"})
	msg := receive(t, conn)

	errBody := msg["error"].(map[string]any)
	if errBody["code"] != string(protocol.CodeUnsupportedVersion) {
		t.Fatalf("code = %v", errBody["code"])
	}
}

func TestPingPongRoundTrip(t *testing.T) {
	srv := wsTestServer(t)
	conn, _ := dial(t, srv.URL+"/ws")

	send(t, conn, map[string]any{"v": 1, "type": "hello", "client": "test"})
	_ = receive(t, conn) // welcome

	send(t, conn, map[string]any{"v": 1, "type": "ping", "nonce": 777})
	msg := receive(t, conn)
	if msg["type"] != "pong" || msg["nonce"].(float64) != 777 {
		t.Fatalf("expected pong with nonce 777, got %v", msg)
	}
}

func TestActionReceivesNotSupportedErrorWithAckSeq(t *testing.T) {
	srv := wsTestServer(t)
	conn, _ := dial(t, srv.URL+"/ws")

	send(t, conn, map[string]any{"v": 1, "type": "hello", "client": "test"})
	_ = receive(t, conn)

	send(t, conn, map[string]any{
		"v": 1, "type": "action", "seq": 12,
		"action": map[string]any{"type": "player_join", "displayName": "Ace"},
	})
	msg := receive(t, conn)

	if msg["type"] != "error" {
		t.Fatalf("expected structured error, got %v", msg)
	}
	errBody := msg["error"].(map[string]any)
	if errBody["code"] != string(protocol.CodeNotSupported) {
		t.Fatalf("code = %v", errBody["code"])
	}
	if msg["ackSeq"].(float64) != 12 {
		t.Fatalf("ackSeq = %v", msg["ackSeq"])
	}
}

func TestMalformedFrameGetsStructuredErrorNotClose(t *testing.T) {
	srv := wsTestServer(t)
	conn, _ := dial(t, srv.URL+"/ws")

	send(t, conn, map[string]any{"v": 1, "type": "hello", "client": "test"})
	_ = receive(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	msg := receive(t, conn)
	if msg["type"] != "error" || msg["error"].(map[string]any)["code"] != string(protocol.CodeMalformedMessage) {
		t.Fatalf("expected malformed_message error, got %v", msg)
	}

	// Connection must remain usable afterwards.
	send(t, conn, map[string]any{"v": 1, "type": "ping", "nonce": 9})
	pong := receive(t, conn)
	if pong["type"] != "pong" {
		t.Fatalf("connection should survive malformed frames; got %v", pong)
	}
}
