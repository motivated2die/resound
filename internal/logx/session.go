package logx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// sessionFile marks a running instance. A clean exit removes it, so finding
// it at start means the previous run ended without one: a native crash Go
// can't report (debug.SetCrashOutput covers Go panics and fatal errors),
// a hang, or the process being ended from outside.
const sessionFile = "running.json"

type session struct {
	Version string    `json:"version"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
}

// StartSession reports how the previous run ended, if not cleanly, and marks
// this one as running. end removes the mark; call it on a clean exit and
// when Windows ends the session (it may end the process without another
// word). Windows' own record of a crash is looked up in the background.
func StartSession(dir, version string) (end func()) {
	path := filepath.Join(dir, sessionFile)
	if data, err := os.ReadFile(path); err == nil {
		var prev session
		if json.Unmarshal(data, &prev) == nil && prev.PID != 0 {
			Printf("previous run: Resound %s (pid %d, started %s) ended without a clean exit",
				prev.Version, prev.PID, prev.Started.Local().Format("2006/01/02 15:04:05"))
			go reportCrash(prev)
		}
	}

	pid := os.Getpid()
	data, _ := json.Marshal(session{Version: version, PID: pid, Started: time.Now()})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		Printf("session mark: %v", err)
	}
	return func() {
		// Only this run's own mark: a later instance may have replaced it
		// (an update that didn't wait for this one to exit).
		data, err := os.ReadFile(path)
		var cur session
		if err == nil && json.Unmarshal(data, &cur) == nil && cur.PID == pid {
			_ = os.Remove(path)
		}
	}
}

// reportCrash logs what Windows recorded about the previous run's end.
func reportCrash(prev session) {
	events, err := crashEvents(prev.PID, prev.Started)
	for _, e := range events {
		Printf("previous run: Windows logged %q: %s", e.Source, e.Details)
	}
	switch {
	case err != nil:
		Printf("previous run: reading Windows' crash records: %v", err)
	case len(events) == 0:
		Printf("previous run: Windows recorded no crash or hang; it was likely ended from outside (Task Manager, power loss)")
	}
}

// crashEvent is a crash or hang Windows logged for a process.
type crashEvent struct {
	Source  string // "Application Error" or "Application Hang"
	Details string // e.g. "ModuleName=ntdll.dll ExceptionCode=0xc0000005 …"
}
