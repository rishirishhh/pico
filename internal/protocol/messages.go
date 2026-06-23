package protocol

import (
	"encoding/json"
	"time"
)

const (
	TypeChat   = "chat"
	TypeVoice  = "voice"
	TypeStroke = "stroke"
	TypeVanish = "vanish"
	TypeJoin   = "join"
	TypeLeave  = "leave"
	TypeClear  = "clear"
)

// Envelope wraps every pubsub payload on the wire.
type Envelope struct {
	Type    string          `json:"type"`
	Room    string          `json:"room"`
	From    string          `json:"from"`
	TS      int64           `json:"ts"`
	Expires int64           `json:"expires,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func NewEnvelope(msgType, room, from string, payload any, ttl time.Duration) (*Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	env := &Envelope{
		Type:    msgType,
		Room:    room,
		From:    from,
		TS:      time.Now().UnixMilli(),
		Payload: raw,
	}
	if ttl > 0 {
		env.Expires = time.Now().Add(ttl).UnixMilli()
	}
	return env, nil
}

func (e *Envelope) Expired() bool {
	if e.Expires == 0 {
		return false
	}
	return time.Now().UnixMilli() > e.Expires
}

func (e *Envelope) DecodePayload(v any) error {
	return json.Unmarshal(e.Payload, v)
}

func Marshal(env *Envelope) ([]byte, error) {
	return json.Marshal(env)
}

func Unmarshal(data []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

type ChatPayload struct {
	Text string `json:"text"`
}

type VoicePayload struct {
	Chunk      []byte `json:"chunk"`
	Seq        uint32 `json:"seq"`
	SampleRate uint32 `json:"sample_rate,omitempty"`
	Channels   uint16 `json:"channels,omitempty"`
}

type StrokePayload struct {
	ID    string  `json:"id"`
	X0    float64 `json:"x0"`
	Y0    float64 `json:"y0"`
	X1    float64 `json:"x1"`
	Y1    float64 `json:"y1"`
	Color string  `json:"color"`
	Width float64 `json:"width"`
}

type VanishPayload struct {
	Text string `json:"text"`
}

type JoinPayload struct {
	Nickname string `json:"nickname"`
}
