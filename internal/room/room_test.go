package room

import "testing"

func TestIDDeterministic(t *testing.T) {
	a := ID("Golang")
	b := ID("golang")
	if a != b {
		t.Fatalf("expected case-insensitive ids, got %q vs %q", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("expected 16 hex chars, got %q", a)
	}
}

func TestPubSubTopic(t *testing.T) {
	topic := PubSubTopic("music")
	if topic != TopicPrefix+ID("music") {
		t.Fatalf("unexpected topic: %s", topic)
	}
}
