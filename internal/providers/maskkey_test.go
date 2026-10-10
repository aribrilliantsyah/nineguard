package providers

import "testing"

func TestMaskKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "none"},
		{"   ", "none"},
		{"k", "sk-..."},
		{"Bearer k", "sk-..."},
		{"ab", "sk-..."},
		{"abcd", "sk-..."},               // 4 chars: last length with no tail
		{"abcde", "sk-...de"},            // 5 chars: first length with a tail
		{"abcdefghij", "sk-...ij"},       // 10 chars: last of the short branch
		{"abcdefghijk", "abcdef...hijk"}, // 11 chars: long branch
		{"Bearer sk-ng-abcdef123456", "sk-ng-...3456"},
		{"  sk-ng-abcdef123456  ", "sk-ng-...3456"},
	}
	for _, c := range cases {
		if got := maskKey(c.in); got != c.want {
			t.Errorf("maskKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Every length from 0 to 40 must be safe, and a mask must never contain the whole key.
func TestMaskKeyAnyLengthIsSafe(t *testing.T) {
	key := ""
	for n := 0; n <= 40; n++ {
		got := maskKey(key) // must not panic
		if n > 0 && got == key {
			t.Errorf("length %d: mask equals the key", n)
		}
		if n > 0 && n <= 4 && got != "sk-..." {
			t.Errorf("length %d: got %q, want no tail", n, got)
		}
		key += "x"
	}
}
