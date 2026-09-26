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

	"github.com/analeis/highjack/server/internal/match"
	"github.com/analeis/highjack/server/internal/protocol"

	"github.com/coder/websocket"
)

// wsTestServer starts the transport with an empty registry: the v0.1
// handshake/ping/malformed paths are transport-level and need no match.
// Match binding and gameplay are covered by the multiplayer integration
// test in this package.
func wsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	h := NewHandler(log, 30000, match.NewRegistry(log, nil), nil, true)
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

// v0.2: gameplay is implemented, but an unbound connection has no match
// and therefore no actor. Actions are rejected as not_permitted, not
// not_supported (that code now only means "recognized but unimplemented").
func TestActionWithoutMatchBindingIsNotPermitted(t *testing.T) {
	srv := wsTestServer(t)
	conn, _ := dial(t, srv.URL+"/ws")

	send(t, conn, map[string]any{"v": 1, "type": "hello", "client": "test"})
	_ = receive(t, conn) // welcome

	send(t, conn, map[string]any{
		"v": 1, "type": "action", "seq": 12,
		"action": map[string]any{"type": "player_join", "displayName": "Ace"},
	})
	msg := receive(t, conn)

	if msg["type"] != "error" {
		t.Fatalf("expected structured error, got %v", msg)
	}
	errBody := msg["error"].(map[string]any)
	if errBody["code"] != string(protocol.CodeNotPermitted) {
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

// --- origin enforcement ----------------------------------------------------

// The allow-list previously had no test at all, and the behaviour it governs was
// inverted: an empty list set InsecureSkipVerify in *every* environment, so a
// production deployment that omitted HIGHJACK_ALLOWED_ORIGINS had no origin
// enforcement, while the architecture notes claimed the opposite.
func TestWebSocketOriginIsEnforcedOutsideDevelopment(t *testing.T) {
	cases := []struct {
		name       string
		origins    []string
		devMode    bool
		origin     string
		wantStatus int
	}{
		{"production refuses an unlisted origin", nil, false, "https://evil.example", http.StatusForbidden},
		{"production allows an originless client", nil, false, "", 0}, // non-browser: no Origin
		{"development tolerates any origin", nil, true, "https://localhost:5173", 0},
		{"an allow-listed origin is accepted", []string{"https://ok.example"}, false, "https://ok.example", 0},
		{"an unlisted origin is refused against a list", []string{"https://ok.example"}, false, "https://evil.example", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := slog.New(slog.DiscardHandler)
			registry := match.NewRegistry(log, nil)
			h := NewHandler(log, 30000, registry, tc.origins, tc.devMode)
			mux := http.NewServeMux()
			h.Register(mux)
			ts := httptest.NewServer(mux)
			defer ts.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
			opts := &websocket.DialOptions{}
			if tc.origin != "" {
				opts.HTTPHeader = http.Header{"Origin": []string{tc.origin}}
			}
			conn, resp, err := websocket.Dial(ctx, wsURL, opts)
			if tc.wantStatus != 0 {
				if err == nil {
					_ = conn.Close(websocket.StatusNormalClosure, "")
					t.Fatalf("expected the handshake to be refused with %d", tc.wantStatus)
				}
				if resp == nil || resp.StatusCode != tc.wantStatus {
					got := 0
					if resp != nil {
						got = resp.StatusCode
					}
					t.Fatalf("status = %d, want %d", got, tc.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("handshake refused: %v", err)
			}
			_ = conn.Close(websocket.StatusNormalClosure, "")
		})
	}
}
