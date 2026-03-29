package cmd

import (
	"bytes"
	"io"
	"strconv"
	"sync/atomic"
)

// imeCursorWriter wraps an io.Writer and intercepts the trailing cursor-position
// escape sequence that bubbletea emits at the end of every rendered frame
// (\x1b[N;H, where N = last rendered line = bottom of terminal).
//
// On macOS, the IME candidate window is placed below the physical cursor. When
// the cursor is at the very bottom of the terminal, the IME window is pushed
// off-screen.
//
// This writer replaces the escape with \x1b[row;colH using the actual typing
// position reported by the tui model (via cursorPos), so the physical cursor
// tracks the visual cursor and the IME window appears right next to where the
// user is typing.
type imeCursorWriter struct {
	w         io.ReadWriteCloser
	cursorPos *atomic.Int64 // packed (row<<32|col) set by appModel.View() each frame; 0 = unknown
}

func newIMECursorWriter(w io.ReadWriteCloser, cursorPos *atomic.Int64) *imeCursorWriter {
	return &imeCursorWriter{w: w, cursorPos: cursorPos}
}

func (cw *imeCursorWriter) Read(p []byte) (n int, err error) { return cw.w.Read(p) }
func (cw *imeCursorWriter) Close() error                     { return cw.w.Close() }
func (cw *imeCursorWriter) Fd() uintptr {
	type fder interface{ Fd() uintptr }
	if f, ok := cw.w.(fder); ok {
		return f.Fd()
	}
	return 0
}

func (cw *imeCursorWriter) Write(p []byte) (n int, err error) {
	packed := cw.cursorPos.Load()
	row, col := int(packed>>32), int(int32(packed))
	if row > 0 && col > 0 {
		if modified, ok := rewriteTrailingCursorPos(p, row, col); ok {
			_, err = cw.w.Write(modified)
			if err != nil {
				return 0, err
			}
			return len(p), nil
		}
	}
	return cw.w.Write(p)
}

// rewriteTrailingCursorPos finds the trailing \x1b[N;H that bubbletea appends
// after each frame and replaces it with \x1b[row;colH.
// maxEscLen is the maximum byte length of \x1b[row;colH (e.g. \x1b[9999;999H = 12 bytes).
const maxEscLen = 16

func rewriteTrailingCursorPos(p []byte, row, col int) ([]byte, bool) {
	// The escape is always at the very end of the frame buffer; search only the
	// final maxEscLen bytes to avoid scanning the entire frame every render.
	start := len(p) - maxEscLen
	if start < 0 {
		start = 0
	}
	rel := bytes.LastIndex(p[start:], []byte("\x1b["))
	if rel < 0 {
		return nil, false
	}
	idx := start + rel

	after := p[idx+2:]

	// Validate: digits ; H at end of buffer (bubbletea emits \x1b[N;H)
	j := 0
	for j < len(after) && after[j] >= '0' && after[j] <= '9' {
		j++
	}
	if j == 0 || j+2 != len(after) || after[j] != ';' || after[j+1] != 'H' {
		return nil, false
	}

	// Build \x1b[row;colH
	newSeq := strconv.AppendInt([]byte("\x1b["), int64(row), 10)
	newSeq = append(newSeq, ';')
	newSeq = strconv.AppendInt(newSeq, int64(col), 10)
	newSeq = append(newSeq, 'H')

	result := make([]byte, idx+len(newSeq))
	copy(result, p[:idx])
	copy(result[idx:], newSeq)
	return result, true
}
