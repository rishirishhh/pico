package party

import (
	"context"
	"fmt"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rishirishhh/pico/internal/node"
	"github.com/rishirishhh/pico/internal/protocol"
	"github.com/rishirishhh/pico/internal/room"
	"github.com/rishirishhh/pico/internal/voice"
)

// Event is delivered to UI listeners.
type Event struct {
	Kind    string
	From    string
	Room    string
	Text    string
	Stroke  *protocol.StrokePayload
	Vanish  bool
	Expires time.Time
	Voice   bool
}

// Party orchestrates rooms, chat, whiteboard, voice, and vanishing messages.
type Party struct {
	Node   *node.Node
	events chan Event

	mu           sync.RWMutex
	currentRoom  string
	currentTopic string
	cancel       context.CancelFunc

	voice   *voice.Engine
	voiceMu sync.Mutex
}

func New(n *node.Node) *Party {
	return &Party{
		Node:   n,
		events: make(chan Event, 128),
	}
}

func (p *Party) Events() <-chan Event {
	return p.events
}

func (p *Party) CurrentTopic() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.currentTopic
}

// JoinRoom auto-forms (or joins) a topic room over gossipsub + rendezvous.
func (p *Party) JoinRoom(ctx context.Context, topic string) error {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	roomCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.currentTopic = topic
	p.currentRoom = room.PubSubTopic(topic)
	p.mu.Unlock()

	pubTopic := p.currentRoom
	ns := room.RendezvousNamespace()
	key := room.RendezvousKey(topic)

	go func() {
		for {
			p.Node.AdvertiseRoom(roomCtx, ns, key)
			select {
			case <-roomCtx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
	}()

	peers, _ := p.Node.FindRoomPeers(roomCtx, ns, key)
	if len(peers) > 0 {
		fmt.Printf("found %d peer(s) in room %s\n", len(peers), room.Display(topic))
		p.Node.ConnectPeers(roomCtx, peers)
	}

	_, msgCh, err := p.Node.Subscribe(roomCtx, pubTopic)
	if err != nil {
		return fmt.Errorf("subscribe room: %w", err)
	}

	joinEnv, err := protocol.NewEnvelope(protocol.TypeJoin, pubTopic, p.Node.Nickname,
		protocol.JoinPayload{Nickname: p.Node.Nickname}, 0)
	if err != nil {
		return err
	}
	data, _ := protocol.Marshal(joinEnv)
	if err := p.Node.Publish(roomCtx, pubTopic, data); err == nil {
		p.handleMessage(data)
	}

	go p.dispatch(roomCtx, msgCh)
	return nil
}

func (p *Party) LeaveRoom() {
	p.voiceMu.Lock()
	if p.voice != nil {
		p.voice.Stop()
	}
	p.voiceMu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.currentTopic = ""
	p.currentRoom = ""
}

func (p *Party) SendChat(text string) error {
	return p.publish(protocol.TypeChat, protocol.ChatPayload{Text: text}, 0)
}

func (p *Party) SendVanish(text string, ttl time.Duration) error {
	return p.publish(protocol.TypeVanish, protocol.VanishPayload{Text: text}, ttl)
}

func (p *Party) SendStroke(s protocol.StrokePayload) error {
	return p.publish(protocol.TypeStroke, s, 0)
}

func (p *Party) ClearBoard() error {
	return p.publish(protocol.TypeClear, struct{}{}, 0)
}

func (p *Party) SendVoiceChunk(chunk []byte, seq uint32) error {
	pl := protocol.VoicePayload{
		Chunk:      chunk,
		Seq:        seq,
		SampleRate: voice.DefaultFormat.SampleRate,
		Channels:   voice.DefaultFormat.Channels,
	}
	return p.publishNoEcho(protocol.TypeVoice, pl)
}

// StartVoice captures the microphone and sends PCM frames to the current room.
func (p *Party) StartVoice() error {
	if p.CurrentTopic() == "" {
		return fmt.Errorf("join a room first")
	}
	p.voiceMu.Lock()
	defer p.voiceMu.Unlock()

	if p.voice == nil {
		eng, err := voice.NewEngine()
		if err != nil {
			return err
		}
		p.voice = eng
	}

	return p.voice.Start(func(frame []byte, seq uint32) error {
		return p.SendVoiceChunk(frame, seq)
	})
}

// StopVoice stops microphone capture (speaker stays on for incoming voice).
func (p *Party) StopVoice() {
	p.voiceMu.Lock()
	defer p.voiceMu.Unlock()
	if p.voice != nil {
		p.voice.StopCapture()
	}
}

// VoiceActive reports whether the mic is streaming.
func (p *Party) VoiceActive() bool {
	p.voiceMu.Lock()
	defer p.voiceMu.Unlock()
	return p.voice != nil && p.voice.Running()
}

// Close releases party resources.
func (p *Party) Close() {
	p.voiceMu.Lock()
	if p.voice != nil {
		p.voice.Close()
		p.voice = nil
	}
	p.voiceMu.Unlock()
	p.LeaveRoom()
}

func (p *Party) publishNoEcho(msgType string, payload any) error {
	p.mu.RLock()
	topic := p.currentRoom
	p.mu.RUnlock()
	if topic == "" {
		return fmt.Errorf("not in a room")
	}

	env, err := protocol.NewEnvelope(msgType, topic, p.Node.Nickname, payload, 0)
	if err != nil {
		return err
	}
	data, err := protocol.Marshal(env)
	if err != nil {
		return err
	}
	return p.Node.Publish(context.Background(), topic, data)
}

func (p *Party) publish(msgType string, payload any, ttl time.Duration) error {
	p.mu.RLock()
	topic := p.currentRoom
	p.mu.RUnlock()
	if topic == "" {
		return fmt.Errorf("not in a room")
	}

	env, err := protocol.NewEnvelope(msgType, topic, p.Node.Nickname, payload, ttl)
	if err != nil {
		return err
	}
	data, err := protocol.Marshal(env)
	if err != nil {
		return err
	}
	if err := p.Node.Publish(context.Background(), topic, data); err != nil {
		return err
	}
	// GossipSub does not deliver your own messages back; echo locally so senders see their text.
	p.handleMessage(data)
	return nil
}

func (p *Party) dispatch(ctx context.Context, msgCh <-chan *pubsub.Message) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			// Skip messages we already echoed locally on publish.
			if msg.ReceivedFrom == p.Node.Host.ID() {
				continue
			}
			p.handleMessage(msg.Data)
		}
	}
}

func (p *Party) handleMessage(data []byte) {
	env, err := protocol.Unmarshal(data)
	if err != nil {
		return
	}
	if env.Expired() {
		return
	}

	from := env.From
	if from == "" {
		from = "unknown"
	}

	var expires time.Time
	if env.Expires > 0 {
		expires = time.UnixMilli(env.Expires)
	}

	switch env.Type {
	case protocol.TypeChat:
		var pl protocol.ChatPayload
		if err := env.DecodePayload(&pl); err != nil {
			return
		}
		p.emit(Event{Kind: "chat", From: from, Room: env.Room, Text: pl.Text})

	case protocol.TypeVanish:
		var pl protocol.VanishPayload
		if err := env.DecodePayload(&pl); err != nil {
			return
		}
		ev := Event{Kind: "vanish", From: from, Room: env.Room, Text: pl.Text, Vanish: true, Expires: expires}
		p.emit(ev)
		if env.Expires > 0 {
			go p.scheduleVanishExpiry(from, pl.Text, expires)
		}

	case protocol.TypeStroke:
		var pl protocol.StrokePayload
		if err := env.DecodePayload(&pl); err != nil {
			return
		}
		p.emit(Event{Kind: "stroke", From: from, Room: env.Room, Stroke: &pl})

	case protocol.TypeClear:
		p.emit(Event{Kind: "clear", From: from, Room: env.Room})

	case protocol.TypeJoin:
		var pl protocol.JoinPayload
		if err := env.DecodePayload(&pl); err != nil {
			return
		}
		p.emit(Event{Kind: "join", From: pl.Nickname, Room: env.Room, Text: fmt.Sprintf("%s joined", pl.Nickname)})

	case protocol.TypeLeave:
		p.emit(Event{Kind: "leave", From: from, Room: env.Room, Text: fmt.Sprintf("%s left", from)})

	case protocol.TypeVoice:
		var pl protocol.VoicePayload
		if err := env.DecodePayload(&pl); err != nil {
			return
		}
		if from == p.Node.Nickname {
			return
		}
		p.voiceMu.Lock()
		if p.voice == nil {
			eng, err := voice.NewEngine()
			if err == nil {
				p.voice = eng
			}
		}
		eng := p.voice
		p.voiceMu.Unlock()
		if eng != nil {
			_ = eng.EnsurePlayback()
			eng.Play(pl.Chunk)
		}
	}
}

func (p *Party) scheduleVanishExpiry(from, text string, expires time.Time) {
	wait := time.Until(expires)
	if wait <= 0 {
		return
	}
	time.Sleep(wait)
	p.emit(Event{Kind: "vanish_expired", From: from, Room: "", Text: text, Vanish: true})
}

func (p *Party) emit(ev Event) {
	select {
	case p.events <- ev:
	default:
	}
}
