package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two events as wevtutil prints them: Resound's crash (pid 0x2574) and
// another app's.
const crashXML = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='Application Error'/><EventID>1000</EventID></System><EventData><Data Name='AppName'>Resound.exe</Data><Data Name='AppVersion'>0.15.0.0</Data><Data Name='AppTimeStamp'>62712fd7</Data><Data Name='ModuleName'>AudioSes.dll</Data><Data Name='ModuleVersion'>10.0.26100.1</Data><Data Name='ExceptionCode'>c0000005</Data><Data Name='FaultingOffset'>00031f07</Data><Data Name='ProcessId'>0x2574</Data><Data Name='ModulePath'>C:\Windows\System32\AudioSes.dll</Data><Data Name='PackageFullName'></Data></EventData></Event>` +
	`<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='Application Hang'/><EventID>1002</EventID></System><EventData><Data Name='AppName'>dllhost.exe</Data><Data Name='ProcessId'>0x2574</Data><Data Name='HangType'>Quiesce</Data></EventData></Event>`

func TestParseCrashEvents(t *testing.T) {
	events, err := parseCrashEvents([]byte(crashXML), 0x2574)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}
	want := `AppVersion=0.15.0.0 ModuleName=AudioSes.dll ModuleVersion=10.0.26100.1 ExceptionCode=c0000005 FaultingOffset=00031f07 ModulePath=C:\Windows\System32\AudioSes.dll`
	if e := events[0]; e.Source != "Application Error" || e.Details != want {
		t.Errorf("got %+v,\nwant Application Error: %s", e, want)
	}

	if events, err := parseCrashEvents([]byte(crashXML), 1234); err != nil || len(events) != 0 {
		t.Errorf("other pid: got %+v, %v; want none", events, err)
	}
	if events, err := parseCrashEvents(nil, 1); err != nil || len(events) != 0 {
		t.Errorf("no output: got %+v, %v; want none", events, err)
	}
}

func TestSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sessionFile)

	end := StartSession(dir, "1.0.0")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no mark while running: %v", err)
	}
	end()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("mark left after a clean exit: %v", err)
	}

	// A later instance's mark stays when an older one exits.
	end = StartSession(dir, "1.0.0")
	if err := os.WriteFile(path, []byte(`{"version":"1.0.1","pid":999999999}`), 0o644); err != nil {
		t.Fatal(err)
	}
	end()
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "999999999") {
		t.Errorf("another run's mark was removed: %q, %v", data, err)
	}
}
