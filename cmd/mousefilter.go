package cmd

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// mouseArtifactFilter suppresses raw SGR mouse escape sequence fragments that
// leak through bubbletea's input parser as tea.KeyMsg events. When the parser
// fails to recognize a mouse sequence (ESC [ < N;N;N m), the bytes arrive as
// individual KeyMsg: "[", "<", digits, ";", "m" etc. This filter tracks state
// across Update calls to detect and suppress these fragments.
type mouseArtifactFilter struct {
	state mouseFilterState
}

type mouseFilterState int

const (
	mfIdle       mouseFilterState = iota
	mfGotBracket                  // saw "[" — might be start of mouse sequence
	mfInSequence                  // saw "[<" — consuming digits/semicolons until m/M
)

func newMouseArtifactFilter() *mouseArtifactFilter {
	return &mouseArtifactFilter{}
}

func (f *mouseArtifactFilter) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		f.state = mfIdle
		return msg
	}

	s := keyMsg.String()
	runes := keyMsg.Runes

	switch f.state {
	case mfIdle:
		// Check for multi-char artifacts that contain the full or partial sequence
		raw := string(runes)
		if raw != "" && containsMouseEscFragment(raw) {
			return nil
		}
		// Single "[" might be start of a broken mouse sequence
		if s == "[" || s == "alt+[" {
			f.state = mfGotBracket
			return nil // suppress; will be re-evaluated
		}

	case mfGotBracket:
		// After "[", expect "<" for SGR mouse sequence
		if s == "<" || (len(runes) == 1 && runes[0] == '<') {
			f.state = mfInSequence
			return nil
		}
		// Not a mouse sequence — reset but don't suppress.
		// The "[" was already consumed, so we let this character through.
		// This means a genuine "[" typed before a non-mouse char is lost,
		// but in practice mouse artifacts arrive in rapid bursts where
		// there's no user input interleaved.
		f.state = mfIdle
		return msg

	case mfInSequence:
		// Consuming digits, semicolons until we see m/M (end of SGR sequence)
		if len(runes) == 1 {
			ch := runes[0]
			if ch >= '0' && ch <= '9' || ch == ';' {
				return nil // still in sequence, suppress
			}
			if ch == 'm' || ch == 'M' {
				f.state = mfIdle
				return nil // end of sequence, suppress
			}
		}
		// Also handle multi-char fragments within the sequence
		raw := string(runes)
		if raw != "" && containsMouseEscFragment(raw) {
			return nil
		}
		// Unexpected char — sequence ended abnormally
		f.state = mfIdle
		return msg
	}

	return msg
}

// containsMouseEscFragment checks if a multi-character string looks like
// it contains SGR mouse escape sequence fragments.
func containsMouseEscFragment(raw string) bool {
	raw = strings.TrimPrefix(raw, "\x1b")
	// Full or partial SGR: [<N;N;Nm or <N;N;Nm or N;N;Nm
	for _, prefix := range []string{"[<", "<"} {
		if idx := strings.Index(raw, prefix); idx >= 0 {
			rest := raw[idx+len(prefix):]
			if matchesSGRBody(rest) {
				return true
			}
		}
	}
	// Just digits;digits;digits(m|M) — the [< was consumed earlier
	if matchesSGRBody(raw) {
		return true
	}
	return false
}

func matchesSGRBody(s string) bool {
	i := 0
	semicolons := 0
	hasDigit := false
	for i < len(s) {
		ch := s[i]
		if ch >= '0' && ch <= '9' {
			hasDigit = true
			i++
		} else if ch == ';' {
			semicolons++
			i++
		} else if (ch == 'm' || ch == 'M') && semicolons == 2 && hasDigit {
			return true
		} else {
			return false
		}
	}
	// Partial match (no terminator yet) — still looks like a mouse fragment
	return hasDigit && semicolons >= 1
}
