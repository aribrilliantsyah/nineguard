package ui

import (
	"os"
	"sync"

	"golang.org/x/term"
)

// KeyType identifies the kind of key event detected.
type KeyType string

const (
	KeyUp        KeyType = "UP"
	KeyDown      KeyType = "DOWN"
	KeyLeft      KeyType = "LEFT"
	KeyRight     KeyType = "RIGHT"
	KeyEnter     KeyType = "ENTER"
	KeyEsc       KeyType = "ESC"
	KeyBackspace KeyType = "BACKSPACE"
	KeyCtrlC     KeyType = "CTRL_C"
	KeyRune      KeyType = "RUNE"
)

// KeyEvent represents a keypress.
type KeyEvent struct {
	Type KeyType
	Rune rune
}

// DecodeKeys parses raw terminal input bytes into high-level key events.
func DecodeKeys(buf []byte) []KeyEvent {
	var events []KeyEvent
	i := 0
	for i < len(buf) {
		b := buf[i]

		// Ctrl+C
		if b == 3 {
			events = append(events, KeyEvent{Type: KeyCtrlC})
			i++
			continue
		}

		// Enter (CR or LF)
		if b == '\r' || b == '\n' {
			events = append(events, KeyEvent{Type: KeyEnter})
			i++
			continue
		}

		// Backspace
		if b == 127 || b == 8 {
			events = append(events, KeyEvent{Type: KeyBackspace})
			i++
			continue
		}

		// Escape sequence (e.g. arrow keys)
		if b == 27 {
			if i+2 < len(buf) && (buf[i+1] == '[' || buf[i+1] == 'O') {
				switch buf[i+2] {
				case 'A':
					events = append(events, KeyEvent{Type: KeyUp})
					i += 3
					continue
				case 'B':
					events = append(events, KeyEvent{Type: KeyDown})
					i += 3
					continue
				case 'C':
					events = append(events, KeyEvent{Type: KeyRight})
					i += 3
					continue
				case 'D':
					events = append(events, KeyEvent{Type: KeyLeft})
					i += 3
					continue
				}
			}
			events = append(events, KeyEvent{Type: KeyEsc})
			i++
			continue
		}

		// Navigation aliases (vim/wasd)
		switch rune(b) {
		case 'k', 'K', 'w', 'W':
			events = append(events, KeyEvent{Type: KeyUp, Rune: rune(b)})
		case 'j', 'J', 's', 'S':
			events = append(events, KeyEvent{Type: KeyDown, Rune: rune(b)})
		default:
			events = append(events, KeyEvent{Type: KeyRune, Rune: rune(b)})
		}
		i++
	}
	return events
}

// Global single key reader instance
type KeyReader struct {
	events chan KeyEvent
	close  sync.Once
	done   chan struct{}
}

var (
	globalReader     *KeyReader
	globalReaderOnce sync.Once
)

// GetKeyReader returns the singleton KeyReader ensuring only one goroutine reads stdin.
func GetKeyReader() *KeyReader {
	globalReaderOnce.Do(func() {
		globalReader = &KeyReader{
			events: make(chan KeyEvent, 64),
			done:   make(chan struct{}),
		}
		go globalReader.loop()
	})
	return globalReader
}

func (kr *KeyReader) loop() {
	var buf [64]byte
	for {
		select {
		case <-kr.done:
			return
		default:
		}

		n, err := os.Stdin.Read(buf[:])
		if err != nil || n == 0 {
			return
		}

		evs := DecodeKeys(buf[:n])
		for _, ev := range evs {
			select {
			case kr.events <- ev:
			case <-kr.done:
				return
			}
		}
	}
}

// Events returns the channel receiving key events.
func (kr *KeyReader) Events() <-chan KeyEvent {
	return kr.events
}

// Drain clears any pending queued key events.
func (kr *KeyReader) Drain() {
	for {
		select {
		case <-kr.events:
		default:
			return
		}
	}
}

// TerminalSession manages raw mode.
type TerminalSession struct {
	oldState *term.State
	fd       int
}

// EnterRawMode puts the terminal into raw mode.
func EnterRawMode() (*TerminalSession, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return &TerminalSession{oldState: oldState, fd: fd}, nil
}

// Restore restores cooked mode.
func (s *TerminalSession) Restore() {
	if s != nil && s.oldState != nil {
		_ = term.Restore(s.fd, s.oldState)
	}
}
