package selection

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type ClipboardExecFunc func(name string, args []string, input string) error

func ClipboardCommand(goos string) (string, []string, error) {
	if goos == "darwin" {
		return "pbcopy", nil, nil
	}
	return "", nil, nil
}

func (f ClipboardExecFunc) Write(text string) error {
	return f.writeForPlatform(runtime.GOOS, text)
}

func (f ClipboardExecFunc) WriteForPlatform(goos, text string) error {
	name, args, err := ClipboardCommand(goos)
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	return f(name, args, text)
}

func (f ClipboardExecFunc) writeForPlatform(goos, text string) error {
	return f.WriteForPlatform(goos, text)
}

func DefaultClipboardExec(name string, args []string, input string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return fmt.Errorf("clipboard command failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("clipboard command failed: %w", err)
	}
	return nil
}

func NewClipboardWriter() ClipboardWriter {
	return ClipboardExecFunc(DefaultClipboardExec)
}
