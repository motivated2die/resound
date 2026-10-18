package logx

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// crashEvents asks the Application event log for the crashes and hangs
// Windows recorded for the process pid since started.
func crashEvents(pid int, started time.Time) ([]crashEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	query := fmt.Sprintf("*[System[(Provider[@Name='Application Error'] or Provider[@Name='Application Hang'])"+
		" and TimeCreated[@SystemTime>='%s']]]", started.UTC().Format("2006-01-02T15:04:05.000Z"))
	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "wevtutil.exe")
	cmd := exec.CommandContext(ctx, exe, "qe", "Application", "/q:"+query, "/f:xml", "/c:50")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseCrashEvents(out, pid)
}

// crashFields are the parts of an event worth logging, in this order.
var crashFields = []string{"AppVersion", "ModuleName", "ModuleVersion", "ExceptionCode", "FaultingOffset", "ModulePath", "HangType"}

// parseCrashEvents picks Resound's events for pid out of wevtutil's XML
// (a sequence of <Event> elements without a root).
func parseCrashEvents(data []byte, pid int) ([]crashEvent, error) {
	type event struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"System>Provider"`
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"EventData>Data"`
	}
	var events []crashEvent
	// wevtutil writes in the console's code page: a non-ASCII path would
	// otherwise stop the decoder.
	data = bytes.ToValidUTF8(data, []byte("?"))
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		var e event
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			return events, nil
		}
		if err != nil {
			return events, err
		}
		fields := map[string]string{}
		for _, d := range e.Data {
			fields[d.Name] = strings.TrimSpace(d.Value)
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(fields["ProcessId"]), "0x"), 16, 64)
		if err != nil || int(id) != pid || !strings.EqualFold(fields["AppName"], "Resound.exe") {
			continue
		}
		var parts []string
		for _, k := range crashFields {
			if v := fields[k]; v != "" {
				parts = append(parts, k+"="+v)
			}
		}
		events = append(events, crashEvent{Source: e.Provider.Name, Details: strings.Join(parts, " ")})
	}
}
