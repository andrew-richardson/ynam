// Package spinner provides a multi-line braille spinner for concurrent CLI
// operations. Each line tracks one independent operation and updates in-place
// using ANSI cursor control. Falls back to plain output when stdout is not a TTY.
package spinner

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

var frames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

type line struct {
	label   string
	done    bool
	failed  bool
	detail  string
	elapsed time.Duration
	start   time.Time
}

// Spinner renders a multi-line braille spinner to stdout.
// Each line tracks the progress of one concurrent operation.
// Safe for concurrent use from multiple goroutines.
type Spinner struct {
	mu    sync.Mutex
	lines []line
	frame int
	tty   bool
	out   io.Writer
	stop  chan struct{}
	wg    sync.WaitGroup
}

// New creates a Spinner with one status line per label.
func New(labels []string) *Spinner {
	lines := make([]line, len(labels))
	for i, l := range labels {
		lines[i] = line{label: l, start: time.Now()}
	}
	return &Spinner{
		lines: lines,
		tty:   term.IsTerminal(int(os.Stdout.Fd())),
		out:   os.Stdout,
		stop:  make(chan struct{}),
	}
}

// Start prints the initial status lines and begins the animation goroutine.
// For non-TTY output it prints a plain "label..." line per entry and returns.
func (s *Spinner) Start() {
	if !s.tty {
		for _, l := range s.lines {
			fmt.Fprintf(s.out, "  %s...\n", l.label)
		}
		return
	}
	for _, l := range s.lines {
		fmt.Fprintf(s.out, "%c %s\n", frames[0], l.label)
	}
	ticker := time.NewTicker(80 * time.Millisecond)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ticker.C:
				s.draw()
			case <-s.stop:
				ticker.Stop()
				s.draw()
				return
			}
		}
	}()
}

// Finish marks line idx as complete. Safe to call from any goroutine.
// On non-TTY output it immediately prints the result line.
func (s *Spinner) Finish(idx int, detail string, err error) {
	s.mu.Lock()
	l := &s.lines[idx]
	l.done = true
	l.failed = err != nil
	l.elapsed = time.Since(l.start)
	if err != nil {
		l.detail = err.Error()
	} else {
		l.detail = detail
	}
	label, det, elapsed, failed := l.label, l.detail, l.elapsed, l.failed
	s.mu.Unlock()

	if !s.tty {
		if failed {
			fmt.Fprintf(s.out, "  ✗ %s — %s\n", label, det)
		} else {
			fmt.Fprintf(s.out, "  ✓ %s — %s (%.1fs)\n", label, det, elapsed.Seconds())
		}
	}
}

// Stop halts the animation goroutine and does a final redraw with a trailing
// blank line. Must be called after all Finish calls are done.
func (s *Spinner) Stop() {
	if !s.tty {
		fmt.Fprintln(s.out)
		return
	}
	close(s.stop)
	s.wg.Wait()
	fmt.Fprintln(s.out)
}

// draw repaints all status lines in-place using ANSI cursor control.
func (s *Spinner) draw() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.frame = (s.frame + 1) % len(frames)

	// Jump cursor back to the first spinner line.
	fmt.Fprintf(s.out, "\033[%dA", len(s.lines))

	for _, l := range s.lines {
		var row string
		switch {
		case l.failed:
			row = fmt.Sprintf("✗ %s — %s", l.label, l.detail)
		case l.done:
			row = fmt.Sprintf("✓ %s — %s (%.1fs)", l.label, l.detail, l.elapsed.Seconds())
		default:
			row = fmt.Sprintf("%c %s", frames[s.frame], l.label)
		}
		// \033[K erases to end of line so leftover characters don't show.
		fmt.Fprintf(s.out, "%s\033[K\n", row)
	}
}
