// Package protocol is the Go-side mirror of @highjack/protocol
// (packages/protocol). It owns the wire constants and message shapes the
// server speaks.
//
// The TypeScript package remains the human-facing source of truth for the
// contract documentation; fixture tests in this package decode the shared
// fixtures under packages/protocol/fixtures so the two implementations
// cannot drift apart silently.
package protocol

// ProtocolVersion follows semver; major must match clients exactly.
const ProtocolVersion = "1.0.0"

// ProtocolMajorVersion is stamped on every message envelope as "v".
const ProtocolMajorVersion = 1

// SchemaVersion identifies the payload schema revision shared via fixtures.
const SchemaVersion = 1

// ErrorCode values match ERROR_CODES in packages/protocol/src/errors.ts.
type ErrorCode string

const (
	CodeMalformedMessage   ErrorCode = "malformed_message"
	CodeUnsupportedVersion ErrorCode = "unsupported_version"
	CodeUnknownMessageType ErrorCode = "unknown_message_type"
	CodeInvalidAction      ErrorCode = "invalid_action"
	CodeNotPermitted       ErrorCode = "not_permitted"
	CodeOutOfPhase         ErrorCode = "out_of_phase"
	CodeGameFull           ErrorCode = "game_full"
	CodeAlreadyStarted     ErrorCode = "already_started"
	CodeRateLimited        ErrorCode = "rate_limited"
	CodeNotSupported       ErrorCode = "not_supported"
	CodeInternalError      ErrorCode = "internal_error"
)
