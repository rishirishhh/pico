package protocol

import (
	"testing"
	"time"
)

func TestEnvelopeExpiry(t *testing.T) {
	env, err := NewEnvelope(TypeChat, "room", "peer1", ChatPayload{Text: "hi"}, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if env.Expired() {
		t.Fatal("should not be expired yet")
	}
	time.Sleep(150 * time.Millisecond)
	if !env.Expired() {
		t.Fatal("should be expired")
	}
}

func TestRoundTrip(t *testing.T) {
	env, err := NewEnvelope(TypeVanish, "room", "peer1", VanishPayload{Text: "secret"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != TypeVanish {
		t.Fatalf("type mismatch: %s", out.Type)
	}
	var pl VanishPayload
	if err := out.DecodePayload(&pl); err != nil {
		t.Fatal(err)
	}
	if pl.Text != "secret" {
		t.Fatalf("text mismatch: %q", pl.Text)
	}
}
