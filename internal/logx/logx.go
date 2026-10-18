// Package logx provides the process-wide logger. It writes to a log file in
// the app data folder and, when a console is attached (debug builds without
// -H=windowsgui), to stderr as well.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
)

var (
	mu     sync.Mutex
	logger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	file   *os.File
)

// Init opens <dir>/debug.log and returns a function that closes it. A log
// over 2 MB moves to debug.old.log first (replacing the one there), so the
// end of the previous run — a crash, say — survives the next start.
func Init(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return func() {}, err
	}
	path := filepath.Join(dir, "debug.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 2<<20 {
		if os.Rename(path, filepath.Join(dir, "debug.old.log")) != nil {
			_ = os.Remove(path)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return func() {}, err
	}
	// Fatal runtime errors and unrecovered panics bypass the logger and go
	// to stderr, which a windowsgui build does not have. Send them to the
	// log file as well.
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		fmt.Fprintf(f, "SetCrashOutput: %v\n", err)
	}
	mu.Lock()
	file = f
	// The file comes first: with -H=windowsgui stderr is invalid and
	// io.MultiWriter stops at the first failing writer.
	logger = log.New(io.MultiWriter(f, os.Stderr), "", log.LstdFlags|log.Lmicroseconds)
	mu.Unlock()
	return func() {
		// SetCrashOutput keeps its own duplicate of the file handle; drop
		// it too, or the log file stays locked (e.g. during uninstall).
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		mu.Lock()
		defer mu.Unlock()
		if file != nil {
			_ = file.Close()
			file = nil
		}
	}, nil
}

// Printf logs a formatted line.
func Printf(format string, args ...any) {
	mu.Lock()
	l := logger
	mu.Unlock()
	_ = l.Output(2, fmt.Sprintf(format, args...))
}
