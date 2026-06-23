package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rishirishhh/pico/internal/board"
	"github.com/rishirishhh/pico/internal/node"
	"github.com/rishirishhh/pico/internal/party"
	"github.com/rishirishhh/pico/internal/protocol"
)

func main() {
	port := flag.Int("port", 0, "TCP listen port (0 = random)")
	nickname := flag.String("name", "", "display name (defaults to peer id)")
	bootstrap := flag.String("bootstrap", "", "comma-separated bootstrap multiaddrs")
	mdns := flag.Bool("mdns", true, "enable local mDNS discovery")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := node.Config{
		ListenPort: *port,
		Nickname:   *nickname,
		EnableMDNS: *mdns,
	}
	if *bootstrap != "" {
		cfg.Bootstrap = strings.Split(*bootstrap, ",")
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 4001
	}

	fmt.Println("P2PParty — decentralized social rooms over libp2p")
	fmt.Println("No servers. Peers connect directly.")

	n, err := node.New(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start node: %v\n", err)
		os.Exit(1)
	}
	defer n.Close()

	fmt.Printf("peer id: %s\n", n.ID())
	fmt.Println("addresses:")
	for _, a := range n.Addrs() {
		fmt.Printf("  %s\n", a)
	}

	p := party.New(n)
	defer p.Close()
	canvas := board.New(80, 20)
	go printEvents(ctx, p, canvas)

	printHelp()
	runREPL(ctx, p, canvas)
}

func printHelp() {
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  /join <topic>              join or auto-create a topic room")
	fmt.Println("  /leave                     leave current room")
	fmt.Println("  /say <message>             send chat message")
	fmt.Println("  /vanish <seconds> <msg>    send message that disappears")
	fmt.Println("  /draw x0 y0 x1 y1 [color]  draw whiteboard stroke (coords 0-1000)")
	fmt.Println("  /board                     show whiteboard")
	fmt.Println("  /clear                     clear whiteboard")
	fmt.Println("  /voice on|off              start/stop microphone (16 kHz PCM over pubsub)")
	fmt.Println("  /peers                     show listen addresses")
	fmt.Println("  /help                      show this help")
	fmt.Println("  /quit                      exit")
	fmt.Println()
}

func runREPL(ctx context.Context, p *party.Party, canvas *board.Canvas) {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		if p.CurrentTopic() != "" {
			fmt.Printf("[%s] > ", p.CurrentTopic())
		} else {
			fmt.Print("> ")
		}

		if !scanner.Scan() {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "/") {
			if p.CurrentTopic() == "" {
				fmt.Println("join a room first: /join <topic>")
				continue
			}
			if err := p.SendChat(line); err != nil {
				fmt.Printf("send failed: %v\n", err)
			}
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])

		switch cmd {
		case "/help", "/h", "/?":
			printHelp()

		case "/quit", "/exit", "/q":
			p.LeaveRoom()
			return

		case "/join":
			if len(parts) < 2 {
				fmt.Println("usage: /join <topic>")
				continue
			}
			topic := strings.Join(parts[1:], " ")
			fmt.Printf("joining room %q...\n", topic)
			if err := p.JoinRoom(ctx, topic); err != nil {
				fmt.Printf("join failed: %v\n", err)
			} else {
				fmt.Printf("in room: %s\n", topic)
			}

		case "/leave":
			p.LeaveRoom()
			canvas.Clear()
			fmt.Println("left room")

		case "/say":
			if len(parts) < 2 {
				fmt.Println("usage: /say <message>")
				continue
			}
			text := strings.Join(parts[1:], " ")
			if err := p.SendChat(text); err != nil {
				fmt.Printf("send failed: %v\n", err)
			}

		case "/vanish":
			if len(parts) < 3 {
				fmt.Println("usage: /vanish <seconds> <message>")
				continue
			}
			secs, err := strconv.Atoi(parts[1])
			if err != nil || secs <= 0 {
				fmt.Println("seconds must be a positive integer")
				continue
			}
			text := strings.Join(parts[2:], " ")
			if err := p.SendVanish(text, time.Duration(secs)*time.Second); err != nil {
				fmt.Printf("send failed: %v\n", err)
			} else {
				fmt.Printf("sent vanishing message (expires in %ds)\n", secs)
			}

		case "/draw":
			if len(parts) < 5 {
				fmt.Println("usage: /draw x0 y0 x1 y1 [color]")
				continue
			}
			x0, _ := strconv.ParseFloat(parts[1], 64)
			y0, _ := strconv.ParseFloat(parts[2], 64)
			x1, _ := strconv.ParseFloat(parts[3], 64)
			y1, _ := strconv.ParseFloat(parts[4], 64)
			color := "white"
			if len(parts) > 5 {
				color = parts[5]
			}
			stroke := protocol.StrokePayload{
				ID:    fmt.Sprintf("%s-%d", p.Node.ID(), time.Now().UnixNano()),
				X0: x0, Y0: y0, X1: x1, Y1: y1,
				Color: color,
				Width: 2,
			}
			if err := p.SendStroke(stroke); err != nil {
				fmt.Printf("draw failed: %v\n", err)
			} else {
				canvas.Add(stroke)
			}

		case "/board":
			fmt.Println("--- whiteboard ---")
			fmt.Print(canvas.Render())
			fmt.Printf("(%s)\n", canvas.Summary())

		case "/clear":
			if err := p.ClearBoard(); err != nil {
				fmt.Printf("clear failed: %v\n", err)
			}
			canvas.Clear()

		case "/voice":
			if len(parts) < 2 {
				if p.VoiceActive() {
					fmt.Println("voice: on (mic streaming)")
				} else {
					fmt.Println("voice: off")
				}
				continue
			}
			switch strings.ToLower(parts[1]) {
			case "on":
				if p.CurrentTopic() == "" {
					fmt.Println("join a room first")
					continue
				}
				if err := p.StartVoice(); err != nil {
					fmt.Printf("voice failed: %v\n", err)
				} else {
					fmt.Println("voice on — mic streaming (16 kHz mono PCM)")
				}
			case "off":
				p.StopVoice()
				fmt.Println("voice off — mic stopped")
			default:
				fmt.Println("usage: /voice on|off")
			}

		case "/peers":
			for _, a := range p.Node.Addrs() {
				fmt.Println(a)
			}

		default:
			fmt.Printf("unknown command: %s (try /help)\n", cmd)
		}
	}
}

func printEvents(ctx context.Context, p *party.Party, canvas *board.Canvas) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-p.Events():
			if !ok {
				return
			}
			switch ev.Kind {
			case "chat":
				fmt.Printf("\n[%s] %s\n> ", ev.From, ev.Text)
			case "vanish":
				if ev.Expires.IsZero() {
					fmt.Printf("\n[%s] 👻 %s: %s (vanishing)\n> ", ev.From, ev.From, ev.Text)
				} else {
					fmt.Printf("\n[%s] 👻 %s: %s (vanishes %s)\n> ", ev.From, ev.From, ev.Text, ev.Expires.Format("15:04:05"))
				}
			case "vanish_expired":
				fmt.Printf("\n[%s] 💨 message vanished: %q\n> ", ev.From, ev.Text)
			case "stroke":
				if ev.Stroke != nil {
					canvas.Add(*ev.Stroke)
					fmt.Printf("\n[%s] drew on whiteboard\n> ", ev.From)
				}
			case "clear":
				canvas.Clear()
				fmt.Printf("\n[%s] cleared whiteboard\n> ", ev.From)
			case "join", "leave":
				fmt.Printf("\n*** %s\n> ", ev.Text)
			case "voice":
				fmt.Printf("\n[%s] 🎤 speaking\n> ", ev.From)
			}
		}
	}
}
