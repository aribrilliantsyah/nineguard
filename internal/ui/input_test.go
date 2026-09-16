package ui

import (
	"testing"
)

func TestDecodeKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []KeyType
	}{
		{
			name:     "arrow up",
			input:    []byte{27, '[', 'A'},
			expected: []KeyType{KeyUp},
		},
		{
			name:     "arrow down",
			input:    []byte{27, '[', 'B'},
			expected: []KeyType{KeyDown},
		},
		{
			name:     "enter",
			input:    []byte{'\r'},
			expected: []KeyType{KeyEnter},
		},
		{
			name:     "escape",
			input:    []byte{27},
			expected: []KeyType{KeyEsc},
		},
		{
			name:     "ctrl-c",
			input:    []byte{3},
			expected: []KeyType{KeyCtrlC},
		},
		{
			name:     "backspace",
			input:    []byte{127},
			expected: []KeyType{KeyBackspace},
		},
		{
			name:     "digit 1",
			input:    []byte{'1'},
			expected: []KeyType{KeyRune},
		},
		{
			name:     "vim k navigation",
			input:    []byte{'k'},
			expected: []KeyType{KeyUp},
		},
		{
			name:     "vim j navigation",
			input:    []byte{'j'},
			expected: []KeyType{KeyDown},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := DecodeKeys(tt.input)
			if len(evs) != len(tt.expected) {
				t.Fatalf("expected %d events, got %d", len(tt.expected), len(evs))
			}
			for i, exp := range tt.expected {
				if evs[i].Type != exp {
					t.Errorf("[%d] expected %s, got %s", i, exp, evs[i].Type)
				}
			}
		})
	}
}
