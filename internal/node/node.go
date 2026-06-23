package node

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/multiformats/go-multiaddr"
)

const VoiceProtocol = "/p2pparty/voice/1.0.0"

// Config holds node bootstrap options.
type Config struct {
	ListenPort  int
	Bootstrap   []string
	EnableMDNS  bool
	Nickname    string
}

// Node is a libp2p host with DHT, pubsub, and discovery.
type Node struct {
	Host      host.Host
	DHT       *dht.IpfsDHT
	PubSub    *pubsub.PubSub
	Discovery *routing.RoutingDiscovery
	Nickname  string

	subs    map[string]*pubsub.Subscription
	subsMu  sync.Mutex
	topics  map[string]*pubsub.Topic
	topicsMu sync.Mutex
	mdns    mdns.Service
}

func New(ctx context.Context, cfg Config) (*Node, error) {
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	limit := rcmgr.InfiniteLimits
	rm, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limit))
	if err != nil {
		return nil, fmt.Errorf("resource manager: %w", err)
	}

	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ResourceManager(rm),
		libp2p.EnableRelay(),
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort)),
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("libp2p host: %w", err)
	}

	kdht, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("dht: %w", err)
	}

	if err := kdht.Bootstrap(ctx); err != nil {
		h.Close()
		return nil, fmt.Errorf("dht bootstrap: %w", err)
	}

	for _, addr := range cfg.Bootstrap {
		maddr, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			continue
		}
		if err := h.Connect(ctx, *info); err == nil {
			fmt.Printf("connected to bootstrap peer %s\n", info.ID.ShortString())
		}
	}

	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithPeerExchange(true),
		pubsub.WithFloodPublish(true),
	)
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("pubsub: %w", err)
	}

	nickname := cfg.Nickname
	if nickname == "" {
		nickname = h.ID().ShortString()
	}

	n := &Node{
		Host:      h,
		DHT:       kdht,
		PubSub:    ps,
		Discovery: routing.NewRoutingDiscovery(kdht),
		Nickname:  nickname,
		subs:      make(map[string]*pubsub.Subscription),
		topics:    make(map[string]*pubsub.Topic),
	}

	if cfg.EnableMDNS {
		svc := mdns.NewMdnsService(h, "p2pparty", &mdnsNotifee{host: h})
		if err := svc.Start(); err != nil {
			h.Close()
			return nil, fmt.Errorf("mdns: %w", err)
		}
		n.mdns = svc
	}

	return n, nil
}

type mdnsNotifee struct {
	host host.Host
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == m.host.ID() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.host.Connect(ctx, pi); err == nil {
		fmt.Printf("mDNS: connected to %s\n", pi.ID.ShortString())
	}
}

func (n *Node) ID() string {
	return n.Host.ID().ShortString()
}

func (n *Node) Addrs() []string {
	var addrs []string
	for _, a := range n.Host.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", a, n.Host.ID()))
	}
	return addrs
}

// getTopic returns the single Topic handle for a gossipsub topic (Join allows one per topic).
func (n *Node) getTopic(topic string) (*pubsub.Topic, error) {
	n.topicsMu.Lock()
	defer n.topicsMu.Unlock()
	if t, ok := n.topics[topic]; ok {
		return t, nil
	}
	t, err := n.PubSub.Join(topic)
	if err != nil {
		return nil, err
	}
	n.topics[topic] = t
	return t, nil
}

// Subscribe joins a gossipsub topic and returns incoming messages.
func (n *Node) Subscribe(ctx context.Context, topic string) (*pubsub.Topic, <-chan *pubsub.Message, error) {
	n.subsMu.Lock()
	defer n.subsMu.Unlock()

	if _, ok := n.subs[topic]; ok {
		return nil, nil, fmt.Errorf("already subscribed to %s", topic)
	}

	t, err := n.getTopic(topic)
	if err != nil {
		return nil, nil, err
	}

	sub, err := t.Subscribe()
	if err != nil {
		return nil, nil, err
	}

	n.subs[topic] = sub
	ch := make(chan *pubsub.Message, 64)

	go func() {
		defer close(ch)
		for {
			msg, err := sub.Next(ctx)
			if err != nil {
				return
			}
			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	return t, ch, nil
}

// Unsubscribe leaves a gossipsub topic.
func (n *Node) Unsubscribe(topic string) error {
	n.subsMu.Lock()
	defer n.subsMu.Unlock()

	sub, ok := n.subs[topic]
	if !ok {
		return nil
	}
	delete(n.subs, topic)
	sub.Cancel()
	return nil
}

// Publish sends data to a gossipsub topic.
func (n *Node) Publish(ctx context.Context, topic string, data []byte) error {
	t, err := n.getTopic(topic)
	if err != nil {
		return err
	}
	return t.Publish(ctx, data)
}

// AdvertiseRoom registers this peer for a rendezvous key so others can find the room.
func (n *Node) AdvertiseRoom(ctx context.Context, ns, key string) {
	util.Advertise(ctx, n.Discovery, ns+"/"+key)
}

// FindRoomPeers discovers peers advertising a rendezvous key.
func (n *Node) FindRoomPeers(ctx context.Context, ns, key string) ([]peer.AddrInfo, error) {
	peerChan, err := n.Discovery.FindPeers(ctx, ns+"/"+key)
	if err != nil {
		return nil, err
	}

	var peers []peer.AddrInfo
	seen := make(map[peer.ID]struct{})
	deadline := time.After(5 * time.Second)

loop:
	for {
		select {
		case pi, ok := <-peerChan:
			if !ok {
				break loop
			}
			if pi.ID == n.Host.ID() {
				continue
			}
			if _, dup := seen[pi.ID]; dup {
				continue
			}
			seen[pi.ID] = struct{}{}
			peers = append(peers, pi)
		case <-deadline:
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	return peers, nil
}

// ConnectPeers dials discovered room peers.
func (n *Node) ConnectPeers(ctx context.Context, peers []peer.AddrInfo) {
	for _, pi := range peers {
		if n.Host.Network().Connectedness(pi.ID) == network.Connected {
			continue
		}
		if err := n.Host.Connect(ctx, pi); err == nil {
			fmt.Printf("  connected to peer %s\n", pi.ID.ShortString())
		}
	}
}

func (n *Node) Close() error {
	n.subsMu.Lock()
	for _, sub := range n.subs {
		sub.Cancel()
	}
	n.subsMu.Unlock()
	if n.mdns != nil {
		_ = n.mdns.Close()
	}
	return n.Host.Close()
}
