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

// fixturesDir resolves the shared wire fixtures owned by packages/protocol.
const fixturesDir = "../../../packages/protocol/fixtures/messages/"

func decodeFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := jsonFixture(fixturesDir + name)
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

func TestDecodeClientHelloFixture(t *testing.T) {
	data := decodeFixture(t, "client_hello.json")
	kind, hello, _, _, perr := protocol.DecodeClientMessage(data)
	if perr != nil {
		t.Fatalf("hello fixture rejected: %+v", perr)
	}
	if kind != protocol.KindHello || hello.Client != "web/0.1.0" {
		t.Fatalf("unexpected hello: %v %+v", kind, hello)
	}
}

func TestDecodePingFixture(t *testing.T) {
	data := decodeFixture(t, "client_ping.json")
	kind, _, ping, _, perr := protocol.DecodeClientMessage(data)
	if perr != nil {
		t.Fatalf("ping fixture rejected: %+v", perr)
	}
	if kind != protocol.KindPing || ping.Nonce != 42 {
		t.Fatalf("unexpected ping: %v %+v", kind, ping)
	}
}

func TestDecodeActionFixtureStaysOpaque(t *testing.T) {
	data := decodeFixture(t, "client_action_join.json")
	kind, _, _, action, perr := protocol.DecodeClientMessage(data)
	if perr != nil {
		t.Fatalf("action fixture rejected: %+v", perr)
	}
	if kind != protocol.KindAction {
		t.Fatalf("kind = %v", kind)
	}
	if action.Seq != 7 {
		t.Fatalf("seq = %d", action.Seq)
	}
	var payload map[string]any
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "player_join" || payload["displayName"] != "Ace" {
		t.Fatalf("payload mismatch: %v", payload)
	}
}

func TestDecodeRejectsMalformedAndUnknown(t *testing.T) {
	cases := []struct {
		name string
		data string
		want protocol.ErrorCode
	}{
		{"not json", `{{{`, protocol.CodeMalformedMessage},
		{"array frame", `[1,2]`, protocol.CodeMalformedMessage},
		{"missing v", `{"type":"ping","nonce":1}`, protocol.CodeMalformedMessage},
		{"wrong v", `{"v":99,"type":"ping","nonce":1}`, protocol.CodeUnsupportedVersion},
		{"missing type", `{"v":1,"nonce":1}`, protocol.CodeMalformedMessage},
		{"unknown type", `{"v":1,"type":"teleport"}`, protocol.CodeUnknownMessageType},
		{"hello empty client", `{"v":1,"type":"hello","client":""}`, protocol.CodeMalformedMessage},
		{"action missing seq", `{"v":1,"type":"action","action":{}}`, protocol.CodeMalformedMessage},
		{"ping missing nonce", `{"v":1,"type":"ping"}`, protocol.CodeMalformedMessage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, perr := protocol.DecodeClientMessage([]byte(tc.data))
			if perr == nil {
				t.Fatal("expected rejection")
			}
			if perr.Code != tc.want {
				t.Fatalf("code = %s, want %s (msg: %s)", perr.Code, tc.want, perr.Msg)
			}
		})
	}
}

// The fixture test that used to live here marshalled a map built inside the test
// and compared it to the fixture, so it never touched production encoding: it
// would have passed with the session key deleted or the heartbeat renamed. The
// test below drives a real connection through the real handler and diffs the
// bytes the server actually writes against the shared fixture, which is the only
// version of this assertion that can fail for a production reason.
func TestServerFramesMatchSharedFixtures(t *testing.T) {
	// Built directly against the handler rather than through api.New: this file is
	// in package realtime, and api imports realtime, so mounting the full server
	// here would be an import cycle. The handler is the thing that encodes frames,
	// so this still exercises production encoding.
	log := slog.New(slog.DiscardHandler)
	registry := match.NewRegistry(log, nil)
	h := NewHandler(log, 30000, registry, nil)
	mux := http.NewServeMux()
	h.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

	send := func(v any) {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("non-JSON frame: %v", err)
		}
		return m
	}

	// --- welcome -----------------------------------------------------------
	send(map[string]any{"v": 1, "type": "hello", "client": "test/0.2.0"})
	got := read()
	if got["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", got)
	}
	assertMatchesFixture(t, "server_welcome.json", got,
		// The session id is minted per connection, so it cannot be pinned.
		map[string]bool{"session": true})

	// --- pong --------------------------------------------------------------
	// The nonce is an integer on the wire; the shared fixtures pin 42.
	send(map[string]any{"v": 1, "type": "ping", "nonce": 42})
	got = read()
	if got["type"] != "pong" {
		t.Fatalf("expected pong, got %v", got)
	}
	assertMatchesFixture(t, "server_pong.json", got, nil)

	// --- error -------------------------------------------------------------
	// A second hello on the same connection is refused with a structured error,
	// which is the cheapest deterministic way to make the server emit one without
	// a bound match.
	send(map[string]any{"v": 1, "type": "hello", "client": "test/0.2.0"})
	got = read()
	if got["type"] != "error" {
		t.Fatalf("expected the duplicate hello to be refused, got %v", got)
	}
	assertMatchesFixtureShape(t, "server_error.json", got,
		map[string]bool{"ackSeq": true, "error": true})
}

// assertMatchesFixture compares every fixture key against the frame the server
// actually wrote. Keys listed in ignore are present in one and not the other.
func assertMatchesFixture(t *testing.T, name string, got map[string]any, ignore map[string]bool) {
	t.Helper()
	var want map[string]any
	loadJSON(t, fixturesDir+name, &want)
	for key, wv := range want {
		if ignore[key] {
			continue
		}
		gv, ok := got[key]
		if !ok {
			t.Fatalf("%s: server frame is missing fixture key %q (frame: %v)", name, key, got)
		}
		if normalizeNumber(wv) != normalizeNumber(gv) {
			t.Fatalf("%s: key %q diverged: fixture=%v server=%v", name, key, wv, gv)
		}
	}
	for key := range ignore {
		if _, ok := got[key]; !ok {
			t.Fatalf("%s: server frame is missing key %q that it should carry", name, key)
		}
	}
}

// assertMatchesFixtureShape checks the fixture's key set and the frame's key set
// agree, allowing the listed keys to differ, for frames whose values are
// inherently per-connection.
func assertMatchesFixtureShape(t *testing.T, name string, got map[string]any, variable map[string]bool) {
	t.Helper()
	var want map[string]any
	loadJSON(t, fixturesDir+name, &want)
	for key := range want {
		if variable[key] {
			continue
		}
		if _, ok := got[key]; !ok {
			t.Fatalf("%s: server frame is missing fixture key %q (frame: %v)", name, key, got)
		}
	}
	inner, _ := got["error"].(map[string]any)
	if inner == nil {
		t.Fatalf("%s: server error frame has no error object: %v", name, got)
	}
	for key := range want["error"].(map[string]any) {
		if _, ok := inner[key]; !ok {
			t.Fatalf("%s: server error object is missing key %q: %v", name, key, inner)
		}
	}
}

func loadJSON(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := jsonFixture(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatal(err)
	}
}

func normalizeNumber(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}
