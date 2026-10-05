package scan

import (
	"fmt"
	"io"
	"os"
	"time"
)

// spinnerFrames is the braille rotation used for progress on a terminal.
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

const spinnerInterval = 100 * time.Millisecond

// spinner shows a rotating marker on a terminal while a slow task runs. It
// writes nothing to a non-terminal, so piped and CI output stays clean.
type spinner struct {
	out    io.Writer
	done   chan struct{}
	finish chan struct{}
}

func startSpinner(out io.Writer, label string) *spinner {
	s := &spinner{out: out, done: make(chan struct{}), finish: make(chan struct{})}
	if !isTerminal(out) {
		close(s.finish)
		return s
	}
	go s.run(label)
	return s
}

func (s *spinner) run(label string) {
	defer close(s.finish)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-s.done:
			fmt.Fprint(s.out, "\r\033[K")
			return
		case <-ticker.C:
			fmt.Fprintf(s.out, "\r%c %s", spinnerFrames[i%len(spinnerFrames)], label)
			i++
		}
	}
}

// Stop ends the animation and clears its line. Only the spinner goroutine
// writes to the stream, so there is no write race.
func (s *spinner) Stop() {
	close(s.done)
	<-s.finish
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
