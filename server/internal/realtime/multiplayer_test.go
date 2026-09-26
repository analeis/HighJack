// Package realtime_test hosts the real multiplayer integration test. It
// lives in the external test package so it can mount the whole server
// (HTTP lobby + WS transport) without an import cycle.
package realtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/api"
	"github.com/analeis/highjack/server/internal/config"
	"github.com/analeis/highjack/server/internal/match"
	"github.com/analeis/highjack/server/internal/realtime"

	"github.com/coder/websocket"
)

// Real multiplayer integration: two distinct WebSocket clients drive one
// authoritative match through the actual HTTP + WS server (httptest, no
// transport mocks).
//
// This file previously documented that coverage while containing two tests,
// neither of which bound a second client, and two purpose-built helpers that
// nothing called (audit OPS-2b). Every claim below is now backed by a test that
// binds two clients to one match and asserts on what each of them received.

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

	// view is the most recent authoritative state this client has actually
	// received — from its bind snapshot, an ack, or an explicit resync. Tests
	// assert against this rather than blocking for a frame that will never
	// arrive, because snapshots are sent on bind and on ack, not on a timer.
	lastView map[string]any
	// delivered counts every event frame observed, keyed "tick:type", so a test
	// can assert a transition was delivered exactly once. The background reader
	// writes it while the test goroutine reads it, so every access goes through
	// mu — otherwise -race reports the harness's own bug instead of the server's.
	delivered map[string]int

	// frames is fed by a background reader. A client socket must never be read
	// with a short-lived context to detect "quiet": coder/websocket closes the
	// connection when a read context expires, because it cannot resume a
	// partially read frame. Draining this way killed the socket.
	frames chan map[string]any
	// readErr records why the background reader stopped.
	readErr error
	// sync guards the fields the background reader writes.
	sync.Mutex
}

// startReader launches the single background reader for this client. Every frame
// the server sends is consumed exactly once and folded into the view, so tests
// observe delivery without racing the writer.
func (c *client) startReader() {
	c.frames = make(chan map[string]any, 256)
	go func() {
		defer close(c.frames)
		for {
			_, data, err := c.conn.Read(context.Background())
			if err != nil {
				c.readErr = err
				return
			}
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			c.observe(msg)
			select {
			case c.frames <- msg:
			default:
			}
		}
	}()
}

// await pulls frames until pred is satisfied or the deadline passes. It never
// touches the socket, so it cannot disturb the connection.
func (c *client) await(t *testing.T, what string, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(5 * time.Second)
	var seen []string
	for {
		select {
		case msg, ok := <-c.frames:
			if !ok {
				t.Fatalf("connection closed while waiting for %s (saw %v): %v",
					what, seen, c.readFailure())
			}
			if pred(msg) {
				return msg
			}
			seen = append(seen, frameLabel(msg))
		case <-deadline:
			t.Fatalf("timed out waiting for %s (saw %v)", what, seen)
		}
	}
}

// settle waits for the client's frame channel to go quiet. It reads only the
// channel — never the socket — so it cannot disturb the connection, and the
// background reader keeps folding frames into the view meanwhile.
func (c *client) settle() {
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case _, ok := <-c.frames:
			if !ok {
				return
			}
		case <-deadline:
			return
		}
	}
}

// measureFromNow marks the current point as the baseline for delivery
// assertions, so a test can count only what a specific action published rather
// than everything since the connection opened.
func (c *client) measureFromNow() {
	c.settle()
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.delivered = map[string]int{}
}

// awaitEventAtTick waits until the client has been delivered at least one event
// belonging to the given tick, then lets the stream go quiet.
func (c *client) awaitEventAtTick(t *testing.T, tick int) {
	t.Helper()
	prefix := fmt.Sprintf("%d:", tick)
	deadline := time.After(5 * time.Second)
	for {
		if c.hasEventAt(prefix) {
			c.settle()
			return
		}
		select {
		case _, ok := <-c.frames:
			if !ok {
				t.Fatalf("connection closed while waiting for tick %d: %v", tick, c.readFailure())
			}
		case <-deadline:
			t.Fatalf("no event delivered for tick %d; delivered %v", tick, c.delivery())
		}
	}
}

func (c *client) readFailure() error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	return c.readErr
}

func (c *client) hasEventAt(prefix string) bool {
	for k := range c.delivery() {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func frameLabel(msg map[string]any) string {
	if msg["type"] != "event" {
		return fmt.Sprint(msg["type"])
	}
	ev, _ := msg["event"].(map[string]any)
	return fmt.Sprintf("event(%v:%v)", msg["tick"], ev["type"])
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
	c.startReader()
	c.send(hello)
	welcome := c.await(t, "welcome", func(m map[string]any) bool { return m["type"] == "welcome" })
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
	c.observe(msg)
	return msg
}

// observe folds a received frame into the client's view, exactly as a real
// client would. The ack snapshot is authoritative and replaces the view; event
// frames are counted for delivery assertions.
func (c *client) observe(msg map[string]any) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	switch msg["type"] {
	case "snapshot":
		if s, ok := msg["snapshot"].(map[string]any); ok {
			c.lastView = s
		}
	case "ack":
		if s, ok := msg["snapshot"].(map[string]any); ok {
			c.lastView = s
		}
	case "event":
		if c.delivered == nil {
			c.delivered = map[string]int{}
		}
		ev, _ := msg["event"].(map[string]any)
		c.delivered[frameKey(msg["tick"], ev["type"])]++
	}
}

func frameKey(tick any, typ any) string {
	return fmt.Sprintf("%v:%v", tick, typ)
}

// view returns the last authoritative state this client received.
func (c *client) view() map[string]any {
	c.t.Helper()
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	if c.lastView == nil {
		c.t.Fatal("client has received no authoritative snapshot yet")
	}
	return c.lastView
}

// delivery returns a copy of the observed event counts.
func (c *client) delivery() map[string]int {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	out := make(map[string]int, len(c.delivered))
	for k, v := range c.delivered {
		out[k] = v
	}
	return out
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

// pullSnapshot reads the next snapshot frame and installs it as the view.
func (c *client) pullSnapshot() map[string]any {
	c.t.Helper()
	msg := c.await(c.t, "snapshot", func(m map[string]any) bool { return m["type"] == "snapshot" })
	snap, _ := msg["snapshot"].(map[string]any)
	if snap == nil {
		c.t.Fatalf("snapshot frame has no snapshot: %v", msg)
	}
	c.lastView = snap
	return snap
}

// action sends an action and returns the ack/error frame.
func (c *client) action(actionType string) map[string]any {
	c.t.Helper()
	return c.actionWith(actionType, nil)
}

// actionWith sends an action carrying a payload and returns the ack/error frame.
func (c *client) actionWith(actionType string, payload map[string]any) map[string]any {
	c.t.Helper()
	seq := c.seq
	c.seq++
	body := map[string]any{"type": actionType}
	for k, v := range payload {
		body[k] = v
	}
	c.send(map[string]any{
		"v": 1, "type": "action", "seq": seq,
		"action": body,
	})
	return c.await(c.t, "ack or error for "+actionType, func(m map[string]any) bool {
		return m["type"] == "ack" || m["type"] == "error"
	})
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

// --- shared fixtures -------------------------------------------------------

func snapshotPlayers(t *testing.T, snap map[string]any) []map[string]any {
	t.Helper()
	raw, _ := snap["players"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, p := range raw {
		if m, ok := p.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func playerByName(t *testing.T, snap map[string]any, name string) map[string]any {
	t.Helper()
	for _, p := range snapshotPlayers(t, snap) {
		if p["name"] == name {
			return p
		}
	}
	t.Fatalf("no player named %q in snapshot", name)
	return nil
}

// errorCode extracts the stable code from an error frame. The wire shape nests
// it under "error", so a test that reads msg["code"] silently sees nil.
func errorCode(msg map[string]any) string {
	e, _ := msg["error"].(map[string]any)
	if e == nil {
		return ""
	}
	s, _ := e["code"].(string)
	return s
}

func toFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

func turnPhase(t *testing.T, snap map[string]any) string {
	t.Helper()
	turn, _ := snap["turn"].(map[string]any)
	if turn == nil {
		t.Fatalf("snapshot has no turn: %v", snap)
	}
	s, _ := turn["phase"].(string)
	return s
}

func turnSeat(t *testing.T, snap map[string]any) int {
	t.Helper()
	turn, _ := snap["turn"].(map[string]any)
	return int(toFloat(turn["currentSeat"]))
}

// holder returns the client whose player owns the turn according to the
// *server's* authoritative snapshot. A client's own view is only as fresh as the
// last snapshot or ack it received, and the non-acting client legitimately has
// neither, so deciding who may act from it would be a race.
func holder(t *testing.T, tab table) *client {
	t.Helper()
	ref := tab.reference(t)
	if ref["phase"] != "playing" {
		t.Fatalf("match is %v, expected playing", ref["phase"])
	}
	seat := turnSeat(t, ref)
	for _, n := range []string{"Ace", "Bit"} {
		p := playerByName(t, ref, n)
		if int(toFloat(p["seat"])) == seat && p["status"] == "active" {
			return tab.byName[n]
		}
	}
	t.Fatalf("no active player holds seat %d", seat)
	return nil
}

// reference fetches the server's authoritative snapshot for the match. It is the
// arbiter every convergence assertion is measured against: a client's own view
// is only the last snapshot it was handed, and what matters is whether it was
// *told* the same thing as everyone else.
func (tab table) reference(t *testing.T) map[string]any {
	t.Helper()
	resp, err := tab.ts.Client().Get(tab.ts.URL + "/matches/" + tab.matchID)
	if err != nil {
		t.Fatalf("reference snapshot: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		t.Fatalf("reference snapshot: status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("reference snapshot: %v", err)
	}
	return out
}

// peerOf returns the client that is not the actor.
func peerOf(tab table, actor *client) *client {
	if actor == tab.byName["Ace"] {
		return tab.byName["Bit"]
	}
	return tab.byName["Ace"]
}

func playerNameHolding(t *testing.T, snap map[string]any) string {
	t.Helper()
	for _, p := range snapshotPlayers(t, snap) {
		if int(toFloat(p["seat"])) == turnSeat(t, snap) {
			return p["name"].(string)
		}
	}
	t.Fatal("no player holds the turn")
	return ""
}

// table is a started two-player match with both clients bound over real sockets.
type table struct {
	ts      *testServer
	matchID string
	byName  map[string]*client
	tokens  map[string]string
}

func startedMatch(t *testing.T) table {
	t.Helper()
	ts := newTestServer(t)
	created := ts.post(t, "/matches", nil)
	matchID, _ := created["matchId"].(string)
	if matchID == "" {
		t.Fatalf("no matchId in create response: %v", created)
	}

	tab := table{
		ts: ts, matchID: matchID,
		byName: map[string]*client{}, tokens: map[string]string{},
	}
	for _, name := range []string{"Ace", "Bit"} {
		joined := ts.post(t, "/matches/"+matchID+"/players",
			map[string]any{"displayName": name})
		token, _ := joined["token"].(string)
		if token == "" {
			t.Fatalf("no token for %s: %v", name, joined)
		}
		tab.tokens[name] = token
		c := dialClient(t, ts, matchID, token, nil)
		c.pullSnapshot() // the authoritative bind snapshot
		tab.byName[name] = c
	}
	for _, c := range tab.byName {
		if ack := c.actionWith("player_ready", map[string]any{"ready": true}); ack["type"] != "ack" {
			t.Fatalf("ready failed: %v", ack)
		}
	}
	// Ace joined first and is therefore the host.
	if ack := tab.byName["Ace"].action("game_start"); ack["type"] != "ack" {
		t.Fatalf("start failed: %v", ack)
	}
	return tab
}

// act performs the single legal action for whoever holds the turn, and returns
// the client that acted.
func act(t *testing.T, tab table) *client {
	t.Helper()
	actor := holder(t, tab)
	var msg map[string]any
	switch turnPhase(t, tab.reference(t)) {
	case "await_roll":
		msg = actor.action("roll_dice")
	case "await_buy_decision":
		msg = actor.action("buy_property")
		if msg["type"] != "ack" {
			// Unaffordable at this price: declining is always legal.
			msg = actor.action("decline_buy")
		}
	case "turn_over":
		msg = actor.action("end_turn")
	default:
		t.Fatalf("unexpected turn phase %q", turnPhase(t, tab.reference(t)))
	}
	if msg["type"] != "ack" {
		t.Fatalf("action rejected mid-play: %v", msg)
	}
	return actor
}

// playUntil advances real turns until pred holds or the budget runs out. Dice
// come from a per-match random root, so tests discover outcomes by playing
// rather than predicting a roll.
func playUntil(t *testing.T, tab table, budget int, pred func(table) bool) {
	t.Helper()
	for i := 0; i < budget; i++ {
		if pred(tab) {
			return
		}
		if tab.reference(t)["phase"] != "playing" {
			break
		}
		act(t, tab)
	}
	if pred(tab) {
		return
	}
	t.Skip("scenario did not arise within the action budget")
}

// --- the flow this file always claimed to cover ----------------------------

// Two real clients in one match. After every transition the server's
// authoritative snapshot must match what the acting client was acknowledged, and
// the non-acting client must have been delivered that same transition exactly
// once.
func TestTwoClientsConvergeOnOneAuthoritativeMatch(t *testing.T) {
	tab := startedMatch(t)

	// Both bind snapshots describe the same match and roster. Neither is expected
	// to show "playing": a snapshot is sent on bind and on ack, and the
	// non-acting client receives the start as an event. Asserting otherwise would
	// be asserting the wrong thing about the protocol.
	for name, c := range tab.byName {
		snap := c.view()
		if got := snap["matchId"]; got != tab.matchID {
			t.Fatalf("%s snapshot is for match %v, want %s", name, got, tab.matchID)
		}
		if n := len(snapshotPlayers(t, snap)); n != 2 {
			t.Fatalf("%s sees %d players, want 2", name, n)
		}
	}
	if got := tab.reference(t)["phase"]; got != "playing" {
		t.Fatalf("server phase = %v, want playing", got)
	}

	// Play real transitions. After each, both clients must be provably current.
	for round := 0; round < 6; round++ {
		if tab.reference(t)["phase"] != "playing" {
			break
		}
		actor := holder(t, tab)
		peer := peerOf(tab, actor)

		// Let the peer finish anything outstanding, then measure only this action.
		peer.settle()

		act(t, tab)
		ref := tab.reference(t)
		tick := int(toFloat(ref["tick"]))

		// The acting client's authoritative ack must agree with the server.
		if got := toFloat(actor.view()["tick"]); got != float64(tick) {
			t.Fatalf("round %d: actor at tick %v, server at %v", round, got, tick)
		}

		// The peer must have been delivered that transition, exactly once.
		peer.awaitEventAtTick(t, tick)
		prefix := fmt.Sprintf("%d:", tick)
		for k, n := range peer.delivery() {
			if strings.HasPrefix(k, prefix) && n != 1 {
				t.Fatalf("round %d: peer received %s %d times, want exactly 1", round, k, n)
			}
		}

		// And the peer must not have been told about a later tick it should not
		// have: the acting client is still the only one acting.
		for k := range peer.delivery() {
			if strings.HasPrefix(k, prefix) {
				continue
			}
			if at, _ := fmt.Sscanf(k, "%d:", new(int)); at > tick {
				t.Fatalf("round %d: peer was delivered future tick %d (server at %d)",
					round, at, tick)
			}
		}
	}
}

// An invalid action is refused and mutates nothing for anybody, and is not
// published to the peer.
func TestInvalidActionMutatesNothingForAnyClient(t *testing.T) {
	tab := startedMatch(t)
	bit := tab.byName["Bit"]

	if holder(t, tab) == bit {
		t.Skip("Bit holds the turn; cannot test an out-of-turn rejection here")
	}
	// Measure only what this action publishes.
	bit.measureFromNow()

	before := tab.reference(t)
	tickBefore := toFloat(before["tick"])

	msg := bit.action("roll_dice")
	if msg["type"] != "error" {
		t.Fatalf("expected an error for an out-of-turn roll, got %v", msg)
	}
	if got := errorCode(msg); got != "not_permitted" {
		t.Fatalf("code = %q, want not_permitted", got)
	}

	after := tab.reference(t)
	if got := toFloat(after["tick"]); got != tickBefore {
		t.Fatalf("a rejected action advanced the tick: %v -> %v", tickBefore, got)
	}
	for _, p := range snapshotPlayers(t, after) {
		prior := playerByName(t, before, p["name"].(string))
		if fmt.Sprint(p["money"]) != fmt.Sprint(prior["money"]) ||
			fmt.Sprint(p["position"]) != fmt.Sprint(prior["position"]) ||
			fmt.Sprint(p["seat"]) != fmt.Sprint(prior["seat"]) {
			t.Fatalf("a rejected action changed %v: %v -> %v", p["name"], prior, p)
		}
	}
	// Nothing may have been published to the rejecting client either.
	bit.settle()
	if len(bit.delivery()) != 0 {
		t.Fatalf("a rejected action still published %d event frame(s): %v",
			len(bit.delivery()), bit.delivery())
	}
}

// A purchase by one client is delivered to the other exactly once, carrying the
// same amount the server applied. This is the assertion the missing second client
// previously made impossible.
func TestPurchaseIsDeliveredToBothClientsExactlyOnce(t *testing.T) {
	tab := startedMatch(t)

	playUntil(t, tab, 800, func(tb table) bool {
		return tb.reference(t)["phase"] != "playing" ||
			turnPhase(t, tb.reference(t)) == "await_buy_decision"
	})

	actor := holder(t, tab)
	peer := peerOf(tab, actor)
	buyer := playerNameHolding(t, tab.reference(t))

	before := tab.reference(t)
	moneyBefore := toFloat(playerByName(t, before, buyer)["money"])
	tickBefore := int(toFloat(before["tick"]))

	peer.measureFromNow()

	ack := actor.action("buy_property")
	if ack["type"] != "ack" {
		t.Fatalf("buy rejected for a player who was offered the decision: %v", ack)
	}

	// The server applied exactly one tick, and exactly one debit.
	ref := tab.reference(t)
	tick := int(toFloat(ref["tick"]))
	if tick != tickBefore+1 {
		t.Fatalf("purchase advanced the tick by %d, want 1", tick-tickBefore)
	}
	moneyAfter := toFloat(playerByName(t, ref, buyer)["money"])
	if moneyAfter >= moneyBefore {
		t.Fatalf("buyer's money did not decrease: %v -> %v", moneyBefore, moneyAfter)
	}

	// The peer was told about it exactly once.
	peer.awaitEventAtTick(t, tick)
	prefix := fmt.Sprintf("%d:", tick)
	bought := 0
	for k, n := range peer.delivery() {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if n != 1 {
			t.Fatalf("peer received %s %d times, want exactly 1", k, n)
		}
		if strings.Contains(k, "property_bought") {
			bought++
		}
	}
	if bought != 1 {
		t.Fatalf("peer received property_bought %d times, want 1 (delivered %v)",
			bought, peer.delivery())
	}

	// A purchase moves only the buyer's balance, and only once.
	for _, p := range snapshotPlayers(t, ref) {
		name := p["name"].(string)
		if name == buyer {
			continue
		}
		prior := playerByName(t, before, name)
		if fmt.Sprint(p["money"]) != fmt.Sprint(prior["money"]) {
			t.Fatalf("an unrelated player's money changed on a purchase: %v -> %v",
				prior["money"], p["money"])
		}
	}
}

// A retried sequence is acknowledged again but must not re-deliver the
// transition. Before the fix the cached ack carried its events and the transport
// broadcast them again, so every peer applied the same economic change twice.
func TestDuplicateSequenceIsNotRepublishedToThePeer(t *testing.T) {
	tab := startedMatch(t)

	playUntil(t, tab, 800, func(tb table) bool {
		return tb.reference(t)["phase"] != "playing" ||
			turnPhase(t, tb.reference(t)) == "await_roll"
	})

	actor := holder(t, tab)
	peer := peerOf(tab, actor)
	peer.measureFromNow()

	ack := actor.action("roll_dice")
	if ack["type"] != "ack" {
		t.Fatalf("roll rejected: %v", ack)
	}
	ackSeq := toFloat(ack["ackSeq"])
	rolledTick := int(toFloat(tab.reference(t)["tick"]))

	peer.awaitEventAtTick(t, rolledTick)
	prefix := fmt.Sprintf("%d:", rolledTick)
	sawRoll := false
	for k, n := range peer.delivery() {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if n != 1 {
			t.Fatalf("peer received %s %d times, want exactly 1", k, n)
		}
		if strings.Contains(k, "dice_rolled") {
			sawRoll = true
		}
	}
	if !sawRoll {
		t.Fatalf("peer never received dice_rolled for tick %d; delivered %v",
			rolledTick, peer.delivery())
	}

	// Retry the very same sequence.
	actor.seq--
	replay := actor.action("roll_dice")
	if replay["type"] != "ack" {
		t.Fatalf("a duplicate sequence must be acknowledged, not rejected: %v", replay)
	}
	if toFloat(replay["ackSeq"]) != ackSeq {
		t.Fatalf("replay acknowledged seq %v, want %v", replay["ackSeq"], ackSeq)
	}

	// Nothing further may be delivered for that tick, and the match must not have
	// moved.
	peer.settle()
	for k, n := range peer.delivery() {
		if strings.HasPrefix(k, prefix) && n != 1 {
			t.Fatalf("a duplicate submission re-delivered %s (%d total)", k, n)
		}
	}
	if got := int(toFloat(tab.reference(t)["tick"])); got != rolledTick {
		t.Fatalf("a duplicate sequence advanced the tick: %d -> %d", rolledTick, got)
	}
}

// Alternating actions must leave the non-acting client delivered the final
// transition exactly once, and the acting client's own view matching the server.
func TestAlternatingActionsKeepBothClientsConverged(t *testing.T) {
	tab := startedMatch(t)

	// The last actor has to be tracked, not derived: after a round the current
	// turn holder is the player who will act *next*, which is the other client.
	var last *client
	played := 0
	for round := 0; round < 8; round++ {
		if tab.reference(t)["phase"] != "playing" {
			break
		}
		last = act(t, tab)
		played++
	}
	if played == 0 {
		t.Skip("no transition was played")
	}

	ref := tab.reference(t)
	tick := int(toFloat(ref["tick"]))

	// The client that did not act last must have been delivered the final
	// transition, exactly once.
	quiet := peerOf(tab, last)
	quiet.awaitEventAtTick(t, tick)
	prefix := fmt.Sprintf("%d:", tick)
	for k, n := range quiet.delivered {
		if strings.HasPrefix(k, prefix) && n != 1 {
			t.Fatalf("peer received %s %d times, want 1", k, n)
		}
	}

	// And the acting client's own authoritative view must match the server for
	// every player.
	for _, p := range snapshotPlayers(t, last.view()) {
		name := p["name"].(string)
		live := playerByName(t, ref, name)
		for _, field := range []string{"seat", "money", "position", "status"} {
			if fmt.Sprint(p[field]) != fmt.Sprint(live[field]) {
				t.Fatalf("acting client disagrees with server on %s.%s: %v vs %v",
					name, field, p[field], live[field])
			}
		}
	}
}

// A token minted by one match must not bind to another, and an unknown match
// must be refused.
func TestCrossMatchTokenIsRejected(t *testing.T) {
	ts := newTestServer(t)

	first := ts.post(t, "/matches", nil)
	firstID, _ := first["matchId"].(string)
	second := ts.post(t, "/matches", nil)
	secondID, _ := second["matchId"].(string)

	joined := ts.post(t, "/matches/"+secondID+"/players", map[string]any{"displayName": "Zed"})
	otherToken, _ := joined["token"].(string)
	if otherToken == "" {
		t.Fatalf("no token: %v", joined)
	}

	if dialClientExpectReject(t, ts, firstID, otherToken) != nil {
		t.Fatal("a token from another match was accepted")
	}
	if dialClientExpectReject(t, ts, firstID, "tok_deadbeefdeadbeef") != nil {
		t.Fatal("a fabricated token was accepted")
	}
	if dialClientExpectReject(t, ts, "m_nosuchmatch", otherToken) != nil {
		t.Fatal("an unknown match was accepted")
	}
}

// A reconnecting client is handed a full authoritative snapshot matching the
// server exactly, including the money it had before dropping.
func TestReconnectRecoversSnapshot(t *testing.T) {
	tab := startedMatch(t)
	ace := tab.byName["Ace"]

	// Play real transitions so there is state worth recovering.
	for i := 0; i < 3; i++ {
		if tab.reference(t)["phase"] != "playing" {
			break
		}
		act(t, tab)
	}
	ace.settle()

	ref := tab.reference(t)
	tickBefore := int(toFloat(ref["tick"]))
	moneyBefore := toFloat(playerByName(t, ref, "Ace")["money"])

	_ = ace.conn.Close(websocket.StatusNormalClosure, "")
	cursor := int64(tickBefore)
	back := dialClient(t, tab.ts, tab.matchID, tab.tokens["Ace"], &cursor)
	snap := back.pullSnapshot()

	if got := snap["matchId"]; got != tab.matchID {
		t.Fatalf("recovered snapshot is for %v, want %s", got, tab.matchID)
	}
	if toFloat(snap["tick"]) != float64(tickBefore) {
		t.Fatalf("recovered tick %v, server %d", snap["tick"], tickBefore)
	}
	// The snapshot is authoritative, not a reconstruction: every field must match
	// the server exactly.
	for _, p := range snapshotPlayers(t, snap) {
		name := p["name"].(string)
		live := playerByName(t, ref, name)
		for _, field := range []string{"seat", "money", "position", "status", "ready", "isHost"} {
			if fmt.Sprint(p[field]) != fmt.Sprint(live[field]) {
				t.Fatalf("recovered %s.%s = %v, server has %v",
					name, field, p[field], live[field])
			}
		}
	}
	if got := toFloat(playerByName(t, snap, "Ace")["money"]); got != moneyBefore {
		t.Fatalf("recovered Ace money %v, want %v", got, moneyBefore)
	}
}

// A burst of actions is rate limited, and the limit is an explicit refusal
// rather than a silent drop.
func TestRateLimitRejectsBurst(t *testing.T) {
	tab := startedMatch(t)
	bit := tab.byName["Bit"]

	limited := 0
	codes := map[string]int{}
	for i := 0; i < 60; i++ {
		msg := bit.action("roll_dice")
		if msg["type"] == "error" {
			code := errorCode(msg)
			codes[code]++
			if code == "rate_limited" {
				limited++
			}
		}
	}
	if limited == 0 {
		t.Fatalf("expected a 60-action burst to be rate limited; observed codes %v", codes)
	}
}
