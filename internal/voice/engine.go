package voice

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gen2brain/malgo"
)

const (
	SampleRate   = 16000
	Channels     = 1
	FrameSamples = 320 // 20 ms at 16 kHz
)

// Format describes PCM layout on the wire.
type Format struct {
	SampleRate uint32
	Channels   uint16
}

var DefaultFormat = Format{SampleRate: SampleRate, Channels: Channels}

// Engine captures microphone audio and plays received PCM chunks.
type Engine struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device

	playback playbackBuffer
	seq      atomic.Uint32

	mu      sync.Mutex
	sendFn  func([]byte, uint32) error
	capture bool

	captureCh chan []byte
	done      chan struct{}

	captureLoopOnce sync.Once
}

// NewEngine allocates audio resources.
func NewEngine() (*Engine, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		return nil, fmt.Errorf("audio init: %w", err)
	}
	return &Engine{
		ctx:       ctx,
		captureCh: make(chan []byte, 32),
		done:      make(chan struct{}),
	}, nil
}

// EnsurePlayback opens the speaker if not already active (for receiving voice).
func (e *Engine) EnsurePlayback() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.device != nil {
		return nil
	}
	return e.openDevice(false, nil)
}

// Start opens mic + speaker and streams captured frames through sendFn.
func (e *Engine) Start(sendFn func([]byte, uint32) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sendFn = sendFn
	e.capture = true
	if e.device != nil {
		return nil
	}
	return e.openDevice(true, sendFn)
}

func (e *Engine) openDevice(capture bool, sendFn func([]byte, uint32) error) error {
	e.sendFn = sendFn
	e.capture = capture

	e.captureLoopOnce.Do(func() { go e.captureLoop() })

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Duplex)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = Channels
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = Channels
	deviceConfig.SampleRate = SampleRate
	deviceConfig.Alsa.NoMMap = 1

	onData := func(output, input []byte, _ uint32) {
		if e.capture && len(input) > 0 {
			frame := make([]byte, len(input))
			copy(frame, input)
			select {
			case e.captureCh <- frame:
			default:
			}
		}
		if len(output) > 0 {
			e.playback.read(output)
		}
	}

	device, err := malgo.InitDevice(e.ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onData,
	})
	if err != nil {
		return fmt.Errorf("open audio device: %w", err)
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		return fmt.Errorf("start audio device: %w", err)
	}
	e.device = device
	return nil
}

func (e *Engine) captureLoop() {
	for {
		select {
		case <-e.done:
			return
		case frame, ok := <-e.captureCh:
			if !ok {
				return
			}
			e.mu.Lock()
			fn := e.sendFn
			capture := e.capture
			e.mu.Unlock()
			if !capture || fn == nil {
				continue
			}
			seq := e.seq.Add(1)
			_ = fn(frame, seq)
		}
	}
}

// Play queues PCM from a remote peer for speaker output.
func (e *Engine) Play(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	e.playback.write(pcm)
}

// StopCapture stops the mic; speaker stays open for incoming voice.
func (e *Engine) StopCapture() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.capture = false
	e.sendFn = nil
}

// Stop closes the audio device entirely.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.capture = false
	e.sendFn = nil
	e.closeDeviceLocked()
}

func (e *Engine) closeDeviceLocked() {
	if e.device != nil {
		_ = e.device.Stop()
		e.device.Uninit()
		e.device = nil
	}
}

// Close releases all audio resources.
func (e *Engine) Close() {
	e.mu.Lock()
	e.capture = false
	e.sendFn = nil
	e.closeDeviceLocked()
	e.mu.Unlock()

	select {
	case <-e.done:
	default:
		close(e.done)
	}
	if e.captureCh != nil {
		close(e.captureCh)
		e.captureCh = nil
	}
	if e.ctx != nil {
		e.ctx.Uninit()
		e.ctx = nil
	}
}

// Running reports whether the mic is streaming.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.capture && e.device != nil
}

type playbackBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (p *playbackBuffer) write(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	const maxBuf = SampleRate * Channels * 2 // ~1 s
	p.buf = append(p.buf, data...)
	if len(p.buf) > maxBuf {
		p.buf = append([]byte(nil), p.buf[len(p.buf)-maxBuf:]...)
	}
}

func (p *playbackBuffer) read(dst []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := copy(dst, p.buf)
	for i := n; i < len(dst); i++ {
		dst[i] = 0
	}
	if n > 0 {
		p.buf = p.buf[n:]
	}
}
