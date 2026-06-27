package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// logRotateEvery is how many runs are appended to the log before it is
// overwritten (truncated) and a fresh cycle begins.
const logRotateEvery = 7

// syncLogger tees output to stdout and, optionally, to a log file that is
// overwritten every logRotateEvery runs so it never grows without bound.
type syncLogger struct {
	w    io.Writer
	file *os.File
}

// newSyncLogger returns a logger writing to stdout, plus the file at path when
// path is non-empty. A sidecar "<path>.runcount" tracks how many runs have been
// written; on every logRotateEvery-th run the log is truncated, otherwise it is
// appended to.
func newSyncLogger(path string) (*syncLogger, error) {
	if strings.TrimSpace(path) == "" {
		return &syncLogger{w: os.Stdout}, nil
	}

	truncate := nextRunTruncates(path + ".runcount")

	flags := os.O_CREATE | os.O_WRONLY
	if truncate {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}

	lg := &syncLogger{w: io.MultiWriter(os.Stdout, f), file: f}
	lg.Printf("===== ynam sync %s =====\n", time.Now().Format("2006-01-02 15:04:05"))
	return lg, nil
}

// nextRunTruncates reads and increments the run counter, returning true when
// this run should overwrite the log (the first run of each rotation cycle).
func nextRunTruncates(counterPath string) bool {
	prev := 0
	if b, err := os.ReadFile(counterPath); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			prev = n
		}
	}
	truncate := prev%logRotateEvery == 0
	_ = os.WriteFile(counterPath, []byte(strconv.Itoa(prev+1)), 0644)
	return truncate
}

func (l *syncLogger) Printf(format string, a ...any) {
	fmt.Fprintf(l.w, format, a...)
}

func (l *syncLogger) Println(a ...any) {
	fmt.Fprintln(l.w, a...)
}

func (l *syncLogger) Close() {
	if l.file != nil {
		_ = l.file.Close()
	}
}
