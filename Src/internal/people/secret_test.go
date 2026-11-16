package people

import (
	"io"
	"os"
	"testing"
)

func TestPromptPasswordReadsEachLine(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	old := os.Stdin
	stdinMu.Lock()
	os.Stdin = reader
	stdinReader = nil
	stdinMu.Unlock()
	t.Cleanup(func() {
		stdinMu.Lock()
		os.Stdin = old
		stdinReader = nil
		stdinMu.Unlock()
		writer.Close()
	})
	if _, err := io.WriteString(writer, "alpha-pass\nbravo-pass\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := PromptPassword("")
	if err != nil || first != "alpha-pass" {
		t.Fatalf("%q %v", first, err)
	}
	second, err := PromptPassword("")
	if err != nil || second != "bravo-pass" {
		t.Fatalf("%q %v", second, err)
	}
}
