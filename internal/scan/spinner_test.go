package scan

import (
	"bytes"
	"testing"
)

func TestIsTerminalRejectsBuffer(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("a buffer must not count as a terminal")
	}
}

func TestSpinnerQuietOnNonTerminal(t *testing.T) {
	var out bytes.Buffer
	s := startSpinner(&out, "working...")
	s.Stop()
	if out.Len() != 0 {
		t.Errorf("non-terminal spinner wrote %q", out.String())
	}
}

func TestSpinnerStopIsSafe(t *testing.T) {
	s := startSpinner(&bytes.Buffer{}, "working...")
	s.Stop()
}
