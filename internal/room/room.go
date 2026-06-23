package room

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const TopicPrefix = "p2pparty/room/"

// ID derives a deterministic room identifier from a human topic string.
// Peers joining the same topic land in the same gossipsub channel automatically.
func ID(topic string) string {
	normalized := strings.ToLower(strings.TrimSpace(topic))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

func PubSubTopic(topic string) string {
	return TopicPrefix + ID(topic)
}

func RendezvousNamespace() string {
	return "p2pparty-rooms"
}

func RendezvousKey(topic string) string {
	return ID(topic)
}

func Display(topic string) string {
	return fmt.Sprintf("%s (%s)", strings.TrimSpace(topic), ID(topic))
}
