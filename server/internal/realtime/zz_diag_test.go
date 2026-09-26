package realtime_test

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDiagAck(t *testing.T) {
	ts := newTestServer(t)
	created := ts.post(t, "/matches", nil)
	id, _ := created["matchId"].(string)
	joined := ts.post(t, "/matches/"+id+"/players", map[string]any{"displayName": "Ace"})
	tok, _ := joined["token"].(string)
	c := dialClient(t, ts, id, tok, nil)
	c.pullSnapshot()
	t.Logf("bound; sending ready")
	c.send(map[string]any{"v": 1, "type": "action", "seq": 1,
		"action": map[string]any{"type": "player_ready", "ready": true}})
	select {
	case msg := <-c.frames:
		b, _ := json.Marshal(msg)
		t.Logf("frame: %s", b)
	case <-time.After(3 * time.Second):
		t.Logf("no frame; readErr=%v", c.readFailure())
	}
	select {
	case msg := <-c.frames:
		b, _ := json.Marshal(msg)
		t.Logf("frame2: %s", b)
	case <-time.After(2 * time.Second):
		t.Logf("no second frame; readErr=%v", c.readFailure())
	}
}
