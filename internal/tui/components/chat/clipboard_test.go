package chat

import (
	"errors"
	"strings"
	"testing"
)

func TestClipboardCommandUsesPbcopyOnDarwin(t *testing.T) {
	name, args, err := clipboardCommand("darwin")
	if err != nil {
		t.Fatalf("clipboardCommand returned error: %v", err)
	}
	if name != "pbcopy" {
		t.Fatalf("got %q want pbcopy", name)
	}
	if len(args) != 0 {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestClipboardCommandSkipsOnNonDarwin(t *testing.T) {
	name, args, err := clipboardCommand("linux")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if name != "" {
		t.Fatalf("got command %q want empty", name)
	}
	if args != nil {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestWriteClipboardUsesCommandStdinOnDarwin(t *testing.T) {
	var gotName string
	var gotArgs []string
	var gotInput string
	writer := clipboardExecFunc(func(name string, args []string, input string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		gotInput = input
		return nil
	})

	if err := writer.writeForPlatform("darwin", "copied text"); err != nil {
		t.Fatalf("write returned error: %v", err)
	}
	if gotName != "pbcopy" {
		t.Fatalf("got command %q want pbcopy", gotName)
	}
	if len(gotArgs) != 0 {
		t.Fatalf("unexpected args: %v", gotArgs)
	}
	if gotInput != "copied text" {
		t.Fatalf("got input %q want %q", gotInput, "copied text")
	}
}

func TestWriteClipboardSkipsExecOnNonDarwin(t *testing.T) {
	called := false
	writer := clipboardExecFunc(func(name string, args []string, input string) error {
		called = true
		return nil
	})

	err := writer.writeForPlatform("linux", "copied text")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if called {
		t.Fatal("expected exec function not to be called")
	}
}

func TestWriteClipboardReturnsCommandError(t *testing.T) {
	writer := clipboardExecFunc(func(name string, args []string, input string) error {
		return errors.New("boom")
	})

	err := writer.writeForPlatform("darwin", "copied text")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}
