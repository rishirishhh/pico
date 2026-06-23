# P2PParty

Fully decentralized social app using **libp2p** in Go. Rooms auto-form for topics, with voice, text, shared whiteboards, and ephemeral vanishing content. **No servers needed.**

```
  Peer A                    Peer B                    Peer C
    │                         │                         │
    └──────── gossipsub ──────┴──────── gossipsub ──────┘
              │                         │
         DHT rendezvous            mDNS (LAN)
              │                         │
         direct libp2p streams (voice, hole-punch, relay)
```

## Features

| Feature | How it works |
|---------|--------------|
| **Topic rooms** | Same topic string → same SHA-256 room id → same gossipsub channel. Rooms appear when peers join. |
| **Text chat** | JSON envelopes over GossipSub pubsub. |
| **Vanishing messages** | Messages carry an expiry timestamp; UI drops them when TTL elapses. |
| **Shared whiteboard** | Stroke operations (line segments) broadcast and rendered in the terminal. |
| **Voice** | Real-time mic capture and speaker playback (16 kHz mono PCM) relayed over GossipSub. |
| **Discovery** | Kademlia DHT rendezvous + optional mDNS for LAN + configurable bootstrap peers. |

## Quick start

```bash
make build
./bin/p2pparty -name alice
```

In another terminal (or on another machine):

```bash
./bin/p2pparty -name bob -bootstrap /ip4/127.0.0.1/tcp/4001/p2p/<alice-peer-id>
```

Both peers:

```
/join golang
/say hello from the decentralized web!
/vanish 30 this message disappears in 30 seconds
/draw 100 100 900 900 red
/board
```

## CLI commands

| Command | Description |
|---------|-------------|
| `/join <topic>` | Join or auto-create a room for a topic |
| `/leave` | Leave the current room |
| `/say <msg>` | Send a chat message |
| `/vanish <secs> <msg>` | Send a self-destructing message |
| `/draw x0 y0 x1 y1 [color]` | Draw a line on the shared whiteboard (coords 0–1000) |
| `/board` | Render the whiteboard |
| `/clear` | Clear the whiteboard for everyone |
| `/voice on\|off` | Start/stop microphone streaming |
| `/peers` | Show this node's multiaddrs |
| `/help` | Show help |
| `/quit` | Exit |

Plain text (without `/`) is sent as chat when you are in a room.

## Voice chat

Voice uses **16 kHz mono PCM** streamed over GossipSub (~20 ms frames).

**Terminal 1:**
```bash
./bin/p2pparty -name alice
/join lounge
/voice on
```

**Terminal 2** (same WiFi or with `-bootstrap`):
```bash
./bin/p2pparty -name bob -bootstrap /ip4/192.168.x.x/tcp/4001/p2p/<alice-id>
/join lounge
/voice on
```

Speak into the mic — the other peer hears you through their speakers. `/voice off` stops your mic; incoming audio still plays.

macOS will prompt for **microphone access** — allow it for Terminal/iTerm.

## Flags

```
  -port int        TCP listen port (default 4001)
  -name string     Display name (default: peer id)
  -bootstrap string Comma-separated bootstrap multiaddrs
  -mdns            Enable mDNS LAN discovery (default true)
```

## Architecture

```
cmd/p2pparty/          CLI entrypoint
internal/
  node/                libp2p host, DHT, GossipSub, rendezvous
  room/                Topic → room id mapping
  protocol/            Wire message envelopes
  party/               Room orchestration and event dispatch
  board/               Terminal whiteboard renderer
  voice/               Direct stream voice protocol
```

## Requirements

- Go 1.24+
- No database, no cloud, no central coordinator

## License

MIT
