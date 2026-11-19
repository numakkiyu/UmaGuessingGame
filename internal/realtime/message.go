package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/jsoninput"
	"strings"
	"time"
)

// MessageType is the server notification discriminator. The set is closed:
// contracts/realtime.schema.json enumerates exactly these seven values and
// additionalProperties is false, so an unknown type is a bug on our side, not
// something a client should tolerate.
type MessageType string

const (
	TypeHello          MessageType = "hello"
	TypeStateChanged   MessageType = "state.changed"
	TypePhaseChanged   MessageType = "phase.changed"
	TypePresenceChange MessageType = "presence.changed"
	TypeResyncRequired MessageType = "resync.required"
	TypeSessionRevoked MessageType = "session.revoked"
	TypePong           MessageType = "pong"
)

// SchemaVersion is the only version this package emits.
const SchemaVersion = "1"

// Resource is the snapshot a state.changed notification tells the client to
// re-fetch. It is a closed set in the schema.
type Resource string

const (
	ResourceRoom  Resource = "room"
	ResourceRound Resource = "round"
	ResourceEvent Resource = "event"
)

// ServerMessage is one notification.
//
// Field-by-field correspondence with contracts/realtime.schema.json:
//
//	required: schemaVersion, eventId, type, roomId, roomVersion,
//
// serverEpoch, serverTime, cursor
//
//	optional: resource, roundId, phaseVersion, reason, onlineCount,
//
// heartbeatIntervalMs
//
//	additionalProperties: false
//
// Every field below is therefore either always emitted or gated by a pointer or
// omitempty with a zero value that the schema rejects anyway. Validation is
// centralised in Validate so that tests can assert the contract directly, and so
// that Config.StrictMessages can enforce it in staging.
type ServerMessage struct {
	SchemaVersion string      `json:"schemaVersion"`
	EventID       string      `json:"eventId"`
	Type          MessageType `json:"type"`
	RoomID        string      `json:"roomId"`
	RoomVersion   int64       `json:"roomVersion"`
	ServerEpoch   string      `json:"serverEpoch"`
	ServerTime    string      `json:"serverTime"`
	Cursor        string      `json:"cursor"`

	Resource   Resource `json:"resource,omitempty"`
	RoundID    string   `json:"roundId,omitempty"`
	PhaseVer   *int64   `json:"phaseVersion,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Online     *int64   `json:"onlineCount,omitempty"`
	HeartbeatM *int64   `json:"heartbeatIntervalMs,omitempty"`
}

// Validate reports whether the message satisfies the schema.
//
// The checks mirror the JSON Schema keywords one for one. They are not a general
// JSON Schema implementation; they are the subset the contract actually uses, so
// a contract change that adds a constraint must be reflected here too (and
// TestServerMessageMatchesContractSchema fails if the two drift).
func (m ServerMessage) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("realtime: schemaVersion %q, want %q", m.SchemaVersion, SchemaVersion)
	}
	if err := checkID("eventId", m.EventID); err != nil {
		return err
	}
	switch m.Type {
	case TypeHello, TypeStateChanged, TypePhaseChanged, TypePresenceChange,
		TypeResyncRequired, TypeSessionRevoked, TypePong:
	default:
		return fmt.Errorf("realtime: unknown message type %q", m.Type)
	}
	if err := checkID("roomId", m.RoomID); err != nil {
		return err
	}
	if m.RoomVersion < 0 {
		return fmt.Errorf("realtime: negative roomVersion %d", m.RoomVersion)
	}
	if err := checkID("serverEpoch", m.ServerEpoch); err != nil {
		return err
	}
	if m.ServerTime == "" {
		return errors.New("realtime: empty serverTime")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.ServerTime); err != nil {
		return fmt.Errorf("realtime: serverTime is not RFC3339: %w", err)
	}
	// cursor is required by the schema but has no minLength, so an empty cursor is
	// schema-valid. We still require it non-empty everywhere we build a message,
	// because an empty cursor would force the client into a resync on reconnect
	// for no reason.
	switch m.Type {
	case TypeStateChanged:
		if m.Resource == "" {
			return errors.New("realtime: state.changed requires resource")
		}
		if m.Resource == ResourceRound && m.RoundID == "" {
			return errors.New("realtime: state.changed resource=round requires roundId")
		}
	}
	switch m.Resource {
	case "", ResourceRoom, ResourceRound, ResourceEvent:
	default:
		return fmt.Errorf("realtime: unknown resource %q", m.Resource)
	}
	if m.RoundID != "" {
		if err := checkID("roundId", m.RoundID); err != nil {
			return err
		}
	}
	if m.PhaseVer != nil && *m.PhaseVer < 0 {
		return fmt.Errorf("realtime: negative phaseVersion %d", *m.PhaseVer)
	}
	if m.Online != nil && *m.Online < 0 {
		return fmt.Errorf("realtime: negative onlineCount %d", *m.Online)
	}
	if m.HeartbeatM != nil && *m.HeartbeatM < 1000 {
		return fmt.Errorf("realtime: heartbeatIntervalMs %d below the schema minimum 1000", *m.HeartbeatM)
	}
	return nil
}

// checkID enforces the schema's minLength 1 / maxLength 128 pair.
func checkID(field, v string) error {
	if v == "" {
		return fmt.Errorf("realtime: empty %s", field)
	}
	if len(v) > 128 {
		return fmt.Errorf("realtime: %s longer than 128 bytes", field)
	}
	return nil
}

// PhaseVersion returns the phase version as a plain int64 for callers that do not
// care about presence, and false when the field is absent.
func (m ServerMessage) PhaseVersion() (int64, bool) {
	if m.PhaseVer == nil {
		return 0, false
	}
	return *m.PhaseVer, true
}

// MarshalJSON is the default encoder, kept explicit so that a future refactor
// cannot quietly introduce a field the schema forbids.
func (m ServerMessage) MarshalJSON() ([]byte, error) {
	type alias ServerMessage
	return json.Marshal(alias(m))
}

// ClientMessageType is the closed set of client frames: ping and ack only.
//
// business writes go through REST. A WebSocket frame that looks like a
// guess, a score edit or a role change is a protocol violation and must be
// rejected, not ignored, so the client learns it is talking to the wrong plane.
type ClientMessageType string

const (
	ClientPing ClientMessageType = "ping"
	ClientAck  ClientMessageType = "ack"
)

// ClientMessage is a decoded client frame.
type ClientMessage struct {
	Type            ClientMessageType `json:"type"`
	ClientMessageID string            `json:"clientMessageId,omitempty"`
	SentAt          string            `json:"sentAt,omitempty"`
	Cursor          string            `json:"cursor,omitempty"`
}

// ErrUnknownClientMessage is returned for a frame that is not ping or ack.
var ErrUnknownClientMessage = errors.New("realtime: client message rejected")

// DecodeClientMessage parses and validates one client frame.
//
// The accepted shapes are exactly the two the schema defines:
//
//	{"type":"ping","clientMessageId":"...","sentAt":"RFC3339"}
//	{"type":"ack","cursor":"..."}
//
// Unknown fields are rejected, because a client that sends "action":"submit" or
// "guess":... must be told the frame is invalid rather than have it silently
// dropped. The error text names the reason so the caller can log a metric; it
// never echoes the payload, which could contain answer material.
func DecodeClientMessage(raw []byte) (ClientMessage, error) {
	if err := jsoninput.ValidateObject(raw); err != nil {
		return ClientMessage{}, ErrUnknownClientMessage
	}
	var probe struct {
		Type ClientMessageType `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ClientMessage{}, fmt.Errorf("%w: not JSON", ErrUnknownClientMessage)
	}

	switch probe.Type {
	case ClientPing:
		var m struct {
			Type            ClientMessageType `json:"type"`
			ClientMessageID string            `json:"clientMessageId"`
			SentAt          string            `json:"sentAt"`
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m); err != nil {
			return ClientMessage{}, fmt.Errorf("%w: ping does not match the schema", ErrUnknownClientMessage)
		}
		if m.ClientMessageID == "" || len(m.ClientMessageID) > 128 {
			return ClientMessage{}, fmt.Errorf("%w: ping clientMessageId must be 1–128 characters", ErrUnknownClientMessage)
		}
		if m.SentAt == "" {
			return ClientMessage{}, fmt.Errorf("%w: ping requires sentAt", ErrUnknownClientMessage)
		}
		if _, err := time.Parse(time.RFC3339Nano, m.SentAt); err != nil {
			return ClientMessage{}, fmt.Errorf("%w: ping sentAt is not RFC3339", ErrUnknownClientMessage)
		}
		return ClientMessage{Type: ClientPing, ClientMessageID: m.ClientMessageID, SentAt: m.SentAt}, nil

	case ClientAck:
		var m struct {
			Type   ClientMessageType `json:"type"`
			Cursor string            `json:"cursor"`
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m); err != nil {
			return ClientMessage{}, fmt.Errorf("%w: ack does not match the schema", ErrUnknownClientMessage)
		}
		return ClientMessage{Type: ClientAck, Cursor: m.Cursor}, nil

	default:
		return ClientMessage{}, fmt.Errorf("%w: type %q is not ping or ack; business actions use REST",
			ErrUnknownClientMessage, probe.Type)
	}
}
