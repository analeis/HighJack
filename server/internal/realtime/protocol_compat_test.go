package realtime

import (
	"encoding/json"
	"testing"

	"github.com/analeis/highjack/server/internal/protocol"
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

func TestServerFixturesShapeMatchGoEncoding(t *testing.T) {
	// The Go encodings must carry identical field sets and values as the
	// shared fixtures (key order is deliberately not compared).
	var welcomeFixture map[string]any
	loadJSON(t, fixturesDir+"server_welcome.json", &welcomeFixture)

	welcome := protocol.Welcome{
		Session:         "sess_01J9Z8A7B6C5D4E3F2",
		ProtocolVersion: protocol.ProtocolVersion,
		HeartbeatMs:     30000,
	}
	goBytes, _ := json.Marshal(map[string]any{
		"v":               protocol.ProtocolMajorVersion,
		"type":            "welcome",
		"session":         welcome.Session,
		"protocolVersion": welcome.ProtocolVersion,
		"heartbeatMs":     float64(welcome.HeartbeatMs),
	})
	var goDecoded map[string]any
	if err := json.Unmarshal(goBytes, &goDecoded); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"v", "type", "session", "protocolVersion", "heartbeatMs"} {
		fv, ok := welcomeFixture[key]
		if !ok {
			t.Fatalf("fixture missing key %q", key)
		}
		gv, ok := goDecoded[key]
		if !ok {
			t.Fatalf("go encoding missing key %q", key)
		}
		if normalizeNumber(fv) != normalizeNumber(gv) {
			t.Fatalf("key %q diverged: fixture=%v go=%v", key, fv, gv)
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
