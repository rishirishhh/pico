package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rishirishhh/pico/internal/node"
)

const (
	StreamProtocol = node.VoiceProtocol
	StreamFrameSize = 4096
)

// Stream handles bidirectional voice frames over libp2p streams.
type Stream struct {
	host host.Host
	mu   sync.Mutex
}

func NewStream(h host.Host) *Stream {
	s := &Stream{host: h}
	h.SetStreamHandler(protocol.ID(StreamProtocol), s.handleIncoming)
	return s
}

func (s *Stream) handleIncoming(stream network.Stream) {
	defer stream.Close()
	peerID := stream.Conn().RemotePeer().ShortString()
	for {
		var length uint32
		if err := binary.Read(stream, binary.BigEndian, &length); err != nil {
			return
		}
		if length > StreamFrameSize*4 {
			return
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(stream, buf); err != nil {
			return
		}
		fmt.Printf("[voice-stream] %s: %d bytes\n", peerID, length)
	}
}

// Send opens a stream to a peer and sends one audio frame.
func (s *Stream) Send(ctx context.Context, peerID peer.ID, frame []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stream, err := s.host.NewStream(ctx, peerID, protocol.ID(StreamProtocol))
	if err != nil {
		return err
	}
	defer stream.Close()

	if err := binary.Write(stream, binary.BigEndian, uint32(len(frame))); err != nil {
		return err
	}
	_, err = stream.Write(frame)
	return err
}
