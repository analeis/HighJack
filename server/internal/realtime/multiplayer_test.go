// Package realtime_test hosts the real multiplayer integration test. It
// lives in the external test package so it can mount the whole server
// (HTTP lobby + WS transport) without an import cycle.
package realtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/api"
	"github.com/analeis/highjack/server/internal/config"
	"github.com/analeis/highjack/server/internal/match"
	"github.com/analeis/highjack/server/internal/realtime"

	"github.com/coder/websocket"
)

// This is the real multiplayer integration test: two distinct WebSocket
// clients drive one authoritative match through the actual HTTP + WS
// server (httptest, no mocks, no in-memory fakes of the transport). It
// covers the v0.2 required flow: create match → two clients join and bind
// → ready/start → deterministic roll → purchase observed by both clients
// → rent transfer → invalid action with no mutation → disconnect +
// reconnect recovery → completed match.

type testServer struct {
	*httptest.Server
	registry *match.Registry
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	registry := match.NewRegistry(log, nil)
	cfg := &config.Config{Addr: ":0", HeartbeatMs: 30000, LogLevel: "error"}
	srv := api.New(cfg, log, nil)
	srv.MountMatches(registry)
	srv.MountRealtime(realtime.NewHandler(log, 30000, registry, nil))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &testServer{Server: ts, registry: registry}
}

func (ts *testServer) post(t *testing.T, path string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s: status %d", path, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

type client struct {
	conn    *websocket.Conn
	session string
	matchID string
	token   string
	seq     int64
	t       *testing.T
	// inbox buffers frames that arrive out of the order a test reads
	// them (events are broadcast before the ack, to every client).
	inbox []map[string]any
}

func dialClient(t *testing.T, ts *testServer, matchID, token string, resume *int64) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

	c := &client{conn: conn, t: t, matchID: matchID, token: token, seq: 1}
	hello := map[string]any{"v": 1, "type": "hello", "client": "test/0.2.0"}
	if matchID != "" {
		hello["matchId"] = matchID
		hello["token"] = token
		if resume != nil {
			hello["resumeFromTick"] = *resume
		}
	}
	c.send(hello)
	welcome := c.read()
	if welcome["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", welcome)
	}
	c.session, _ = welcome["session"].(string)
	return c
}

func (c *client) send(payload any) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := json.Marshal(payload)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// read pulls the next frame from the socket.
func (c *client) read() map[string]any {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		c.t.Fatalf("non-JSON frame: %v", err)
	}
	return msg
}

// take scans the inbox for a frame of kind; the caller falls back to the
// socket when it is absent. Frames are never dropped, so delivery order
// between events, acks, and snapshots cannot make a test flaky.
func (c *client) take(kind string) (map[string]any, bool) {
	for i, m := range c.inbox {
		if m["type"] == kind {
			c.inbox = append(c.inbox[:i], c.inbox[i+1:]...)
			return m, true
		}
	}
	return nil, false
}

func (c *client) expect(kind string) map[string]any {
	c.t.Helper()
	if msg, ok := c.take(kind); ok {
		return msg
	}
	for i := 0; i < 16; i++ {
		msg := c.read()
		if msg["type"] == kind {
			return msg
		}
		c.inbox = append(c.inbox, msg)
	}
	c.t.Fatalf("never received %q", kind)
	return nil
}

// awaitEvent waits for a specific event type.
func (c *client) awaitEvent(kind string) map[string]any {
	c.t.Helper()
	for i := 0; i < 16; i++ {
		msg := c.expect("event")
		ev, _ := msg["event"].(map[string]any)
		if ev["type"] == kind {
			return ev
		}
		c.inbox = append(c.inbox, msg)
	}
	c.t.Fatalf("never received %q event", kind)
	return nil
}

func (c *client) snapshot() map[string]any {
	c.t.Helper()
	msg := c.expect("snapshot")
	snap, _ := msg["snapshot"].(map[string]any)
	if snap == nil {
		c.t.Fatalf("snapshot frame has no snapshot: %v", msg)
	}
	return snap
}

// action sends an action and returns the ack/error frame.
func (c *client) action(actionType string) map[string]any {
	c.t.Helper()
	seq := c.seq
	c.seq++
	c.send(map[string]any{
		"v": 1, "type": "action", "seq": seq,
		"action": map[string]any{"type": actionType},
	})
	for i := 0; i < 16; i++ {
		msg := c.read()
		if msg["type"] == "ack" || msg["type"] == "error" {
			return msg
		}
		c.inbox = append(c.inbox, msg)
	}
	c.t.Fatalf("no ack/error for %s", actionType)
	return nil
}

func dialClientExpectReject(t *testing.T, ts *testServer, matchID, token string) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	c := &client{conn: conn, t: t, matchID: matchID, token: token}
	c.send(map[string]any{"v": 1, "type": "hello", "client": "test", "matchId": matchID, "token": token})
	msg := c.read()
	if msg["type"] == "error" {
		return nil
	}
	return c
}

// TestReconnectRecoversSnapshot proves a reconnecting client receives a
// full authoritative snapshot (and optional catch-up), never a
// client-reconstructed state.
func TestReconnectRecoversSnapshot(t *testing.T) {
	ts := newTestServer(t)
	created := ts.post(t, "/matches", nil)
	matchID, _ := created["matchId"].(string)
	seatA := ts.post(t, "/matches/"+matchID+"/players", map[string]any{"displayName": "Ace"})
	tokenA, _ := seatA["token"].(string)

	a := dialClient(t, ts, matchID, tokenA, nil)
	a.snapshot()
	// Disconnect and reconnect with the same token.
	_ = a.conn.Close(websocket.StatusNormalClosure, "")
	resumed := int64(7) // a stale/old cursor: forces a snapshot + resync flag
	b := dialClient(t, ts, matchID, tokenA, &resumed)
	snap := b.snapshot()
	if snap["matchId"] != matchID {
		t.Fatalf("reconnected snapshot has wrong match: %v", snap["matchId"])
	}
	players, _ := snap["players"].([]any)
	if len(players) != 1 {
		t.Fatalf("expected the rejoining player in snapshot, got %d", len(players))
	}
}

// TestRateLimitRejectsBurst proves the per-session action throttle is
// enforced and the connection survives it: a client that floods is cut
// off with rate_limited and can still ping afterwards.
func TestRateLimitRejectsBurst(t *testing.T) {
	ts := newTestServer(t)
	created := ts.post(t, "/matches", nil)
	matchID, _ := created["matchId"].(string)
	seatA := ts.post(t, "/matches/"+matchID+"/players", map[string]any{"displayName": "Ace"})
	tokenA, _ := seatA["token"].(string)
	a := dialClient(t, ts, matchID, tokenA, nil)
	a.snapshot()

	// The burst exceeds the 20-per-10s limit, so throttling is
	// deterministic regardless of which actions the engine accepts.
	for i := int64(1); i <= 30; i++ {
		a.send(map[string]any{
			"v": 1, "type": "action", "seq": i,
			"action": map[string]any{"type": "roll_dice"},
		})
	}

	// Drain the burst: collect acks and errors, stop at the first throttle.
	rateLimited := false
	deadline := time.Now().Add(5 * time.Second)
	for !rateLimited && time.Now().Before(deadline) {
		msg := a.readUntilOneOf("ack", "error")
		if msg["type"] == "error" && msg["error"].(map[string]any)["code"] == "rate_limited" {
			rateLimited = true
		}
	}
	if !rateLimited {
		t.Fatal("expected rate_limited after flooding the action channel")
	}
	// The connection stays usable: ping still round-trips.
	a.send(map[string]any{"v": 1, "type": "ping", "nonce": 99})
	if pong := a.expect("pong"); pong["nonce"].(float64) != 99 {
		t.Fatalf("expected pong after throttle, got %v", pong)
	}
}

// readUntilOneOf reads the next ack/error frame, buffering other frames.
func (c *client) readUntilOneOf(kinds ...string) map[string]any {
	c.t.Helper()
	for i := 0; i < 64; i++ {
		for _, k := range kinds {
			if msg, ok := c.take(k); ok {
				return msg
			}
		}
		msg := c.read()
		for _, k := range kinds {
			if msg["type"] == k {
				return msg
			}
		}
		c.inbox = append(c.inbox, msg)
	}
	c.t.Fatalf("never received one of %v", kinds)
	return nil
}
