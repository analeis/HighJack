package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ClientMessageKind enumerates the message types a client may send.
type ClientMessageKind string

const (
	KindHello  ClientMessageKind = "hello"
	KindPing   ClientMessageKind = "ping"
	KindAction ClientMessageKind = "action"
)

// Hello is the first message every client must send. MatchId/Token are
// optional: omitting both performs a handshake-only connection, which is
// what the v0.1 seam verified.
type Hello struct {
	Client         string `json:"client"`
	MatchID        string `json:"matchId,omitempty"`
	Token          string `json:"token,omitempty"`
	ResumeFromTick *int64 `json:"resumeFromTick,omitempty"`
}

// Ping is answered with a Pong carrying the same nonce.
type Ping struct {
	Nonce int64 `json:"nonce"`
}

// Action carries an opaque game action payload. v0.1 does not implement
// gameplay over WebSockets; actions receive a structured not_supported
// error. The payload stays opaque so the future handler can decode it
// against the engine without changing this seam.
type Action struct {
	Seq     int64             `json:"seq"`
	Payload json.RawMessage   `json:"action"`
	Meta    map[string]string `json:"-"`
}

// Pong answers a ping.
type Pong struct {
	Nonce int64 `json:"nonce"`
}

// Welcome confirms a valid handshake.
type Welcome struct {
	Session         string `json:"session"`
	ProtocolVersion string `json:"protocolVersion"`
	HeartbeatMs     int    `json:"heartbeatMs"`
}

// ErrorBody is the structured error payload.
type ErrorBody struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// ProtocolError reports a failed decode or rejected message.
type ProtocolError struct {
	Code ErrorCode
	Msg  string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("protocol error %s: %s", e.Code, e.Msg)
}

// DecodeClientMessage validates an incoming frame and extracts its typed
// payload for hello/ping; action payloads stay raw. Unknown types yield
// CodeUnknownMessageType, wrong envelopes CodeMalformedMessage.
func DecodeClientMessage(data []byte) (kind ClientMessageKind, hello *Hello, ping *Ping, action *Action, perr *ProtocolError) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(data, &generic); err != nil {
		return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "frame is not a JSON object"}
	}

	vRaw, ok := generic["v"]
	if !ok {
		return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "missing protocol version field \"v\""}
	}
	var v int
	if err := json.Unmarshal(vRaw, &v); err != nil || float64(v) != float64(ProtocolMajorVersion) {
		return "", nil, nil, nil, &ProtocolError{CodeUnsupportedVersion,
			fmt.Sprintf("unsupported protocol version, want %d", ProtocolMajorVersion)}
	}

	typeRaw, ok := generic["type"]
	if !ok {
		return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "missing \"type\" field"}
	}
	var t string
	if err := json.Unmarshal(typeRaw, &t); err != nil {
		return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "\"type\" must be a string"}
	}

	switch ClientMessageKind(t) {
	case KindHello:
		var h Hello
		if err := strictUnmarshal(generic, &h); err != nil || h.Client == "" {
			return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "hello requires a non-empty \"client\""}
		}
		return KindHello, &h, nil, nil, nil
	case KindPing:
		nonceRaw, ok := generic["nonce"]
		if !ok {
			return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "ping requires \"nonce\""}
		}
		var p Ping
		if err := json.Unmarshal(nonceRaw, &p.Nonce); err != nil {
			return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "ping requires integer \"nonce\""}
		}
		return KindPing, nil, &p, nil, nil
	case KindAction:
		var a Action
		seqRaw, ok := generic["seq"]
		if !ok {
			return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "action requires \"seq\""}
		}
		if err := json.Unmarshal(seqRaw, &a.Seq); err != nil {
			return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "\"seq\" must be an integer"}
		}
		actRaw, ok := generic["action"]
		if !ok {
			return "", nil, nil, nil, &ProtocolError{CodeMalformedMessage, "action requires \"action\" payload"}
		}
		a.Payload = actRaw
		return KindAction, nil, nil, &a, nil
	default:
		return "", nil, nil, nil, &ProtocolError{CodeUnknownMessageType,
			fmt.Sprintf("unknown message type %q", t)}
	}
}

func strictUnmarshal(fields map[string]json.RawMessage, into any) error {
	data, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	return dec.Decode(into)
}
