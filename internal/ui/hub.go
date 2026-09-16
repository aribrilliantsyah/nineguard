package ui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// LogEntry represents a single system or traffic log message.
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Source    string
	Message   string
	Attrs     map[string]any
}

// LogHub manages a circular log buffer and broadcasts events to real-time subscribers.
type LogHub struct {
	mu          sync.RWMutex
	ring        []LogEntry
	ringSize    int
	subscribers map[int]chan LogEntry
	nextSubID   int
	echoStdout  atomic.Bool
}

// NewLogHub creates an initialized LogHub.
func NewLogHub(capacity int) *LogHub {
	if capacity <= 0 {
		capacity = 200
	}
	h := &LogHub{
		ring:        make([]LogEntry, 0, capacity),
		ringSize:    capacity,
		subscribers: make(map[int]chan LogEntry),
	}
	h.echoStdout.Store(true) // default: echo to stdout until interactive UI takes over
	return h
}

// SetEchoStdout toggles direct printing of slog logs to standard output.
func (h *LogHub) SetEchoStdout(enabled bool) {
	h.echoStdout.Store(enabled)
}

// IsEchoStdout returns whether logs are directly printed to standard output.
func (h *LogHub) IsEchoStdout() bool {
	return h.echoStdout.Load()
}

// Push records a log entry and fans it out to subscribers and optional stdout.
func (h *LogHub) Push(entry LogEntry) {
	h.mu.Lock()
	if len(h.ring) >= h.ringSize {
		h.ring = h.ring[1:]
	}
	h.ring = append(h.ring, entry)

	// Broadcast to active subscribers
	for _, ch := range h.subscribers {
		select {
		case ch <- entry:
		default:
			// Non-blocking drop if consumer buffer is full
		}
	}
	h.mu.Unlock()

	if h.echoStdout.Load() {
		// Plain text stdout output
		timeStr := entry.Timestamp.Format("15:04:05")
		src := entry.Source
		if src == "" {
			src = "server"
		}
		var attrStr string
		if len(entry.Attrs) > 0 {
			for k, v := range entry.Attrs {
				attrStr += fmt.Sprintf(" %s=%v", k, v)
			}
		}
		fmt.Fprintf(os.Stdout, "%s [%s] [%s] %s%s\n", timeStr, entry.Level, src, entry.Message, attrStr)
	}
}

// Recent returns the recorded log history.
func (h *LogHub) Recent() []LogEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	res := make([]LogEntry, len(h.ring))
	copy(res, h.ring)
	return res
}

// Subscribe returns a new buffered channel for live logs.
func (h *LogHub) Subscribe() (int, chan LogEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSubID++
	id := h.nextSubID
	ch := make(chan LogEntry, 128)
	h.subscribers[id] = ch
	return id, ch
}

// Unsubscribe removes a channel listener.
func (h *LogHub) Unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subscribers[id]; ok {
		close(ch)
		delete(h.subscribers, id)
	}
}

// LogHubHandler bridges Go's slog library into LogHub.
type LogHubHandler struct {
	hub  *LogHub
	next slog.Handler
}

// NewLogHubHandler wraps an existing slog handler and feeds events into the hub.
func NewLogHubHandler(hub *LogHub, next slog.Handler) *LogHubHandler {
	return &LogHubHandler{
		hub:  hub,
		next: next,
	}
}

func (h *LogHubHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *LogHubHandler) Handle(ctx context.Context, r slog.Record) error {
	levelStr := "INFO"
	switch {
	case r.Level < slog.LevelInfo:
		levelStr = "DEBUG"
	case r.Level < slog.LevelWarn:
		levelStr = "INFO"
	case r.Level < slog.LevelError:
		levelStr = "WARN"
	default:
		levelStr = "ERROR"
	}

	source := "server"
	attrs := make(map[string]any)

	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "source" {
			source = a.Value.String()
		} else {
			attrs[a.Key] = a.Value.Any()
		}
		return true
	})

	h.hub.Push(LogEntry{
		Timestamp: r.Time,
		Level:     levelStr,
		Source:    source,
		Message:   r.Message,
		Attrs:     attrs,
	})

	if h.next != nil && h.hub.IsEchoStdout() {
		_ = h.next.Handle(ctx, r)
	}
	return nil
}

func (h *LogHubHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *LogHubHandler) WithGroup(name string) slog.Handler {
	return h
}
