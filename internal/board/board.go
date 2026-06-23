package board

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rishirishhh/pico/internal/protocol"
)

// Canvas holds whiteboard strokes for terminal rendering.
type Canvas struct {
	mu      sync.RWMutex
	strokes []protocol.StrokePayload
	width   int
	height  int
}

func New(width, height int) *Canvas {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return &Canvas{width: width, height: height}
}

func (c *Canvas) Add(s protocol.StrokePayload) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strokes = append(c.strokes, s)
}

func (c *Canvas) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strokes = nil
}

func (c *Canvas) Render() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	grid := make([][]rune, c.height)
	for y := range grid {
		grid[y] = make([]rune, c.width)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}

	for _, s := range c.strokes {
		c.drawLine(grid, s)
	}

	var b strings.Builder
	for _, row := range grid {
		b.WriteString(string(row))
		b.WriteByte('\n')
	}
	return b.String()
}

func (c *Canvas) drawLine(grid [][]rune, s protocol.StrokePayload) {
	x0, y0 := scale(s.X0, s.Y0, c.width, c.height)
	x1, y1 := scale(s.X1, s.Y1, c.width, c.height)
	ch := strokeChar(s.Color)

	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy

	for {
		if y0 >= 0 && y0 < c.height && x0 >= 0 && x0 < c.width {
			grid[y0][x0] = ch
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func scale(x, y float64, w, h int) (int, int) {
	sx := int(x * float64(w-1) / 1000.0)
	sy := int(y * float64(h-1) / 1000.0)
	if sx < 0 {
		sx = 0
	}
	if sy < 0 {
		sy = 0
	}
	if sx >= w {
		sx = w - 1
	}
	if sy >= h {
		sy = h - 1
	}
	return sx, sy
}

func strokeChar(color string) rune {
	switch strings.ToLower(color) {
	case "red", "#f00", "#ff0000":
		return '*'
	case "blue", "#00f", "#0000ff":
		return '+'
	case "green", "#0f0", "#00ff00":
		return '#'
	default:
		return '.'
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (c *Canvas) Summary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("%d stroke(s)", len(c.strokes))
}
