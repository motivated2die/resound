package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/ui"
)

// diagnosticsPage collects what helps when something doesn't work but is
// noise in everyday use: device formats, measured latency, the index, the
// server, and shortcuts to the log and data folders.
func (a *App) diagnosticsPage() []ui.Row {
	rows := []ui.Row{ui.Header("Diagnostics", a.back), ui.Section("Audio outputs")}
	for _, s := range a.engine.Status() {
		label := roleLabel(s.Role)
		switch {
		case s.Running:
			rows = append(rows,
				ui.Text(label, s.Info.DeviceName, ui.ToneGood),
				ui.Text("", fmt.Sprintf("%s · buffer %d ms · latency %.0f ms", s.FormatText(),
					s.Info.Buffer.Milliseconds(), ms(s.Info.StreamLatency)), ui.ToneMuted))
		case s.Err != nil:
			rows = append(rows, ui.Paragraph(label+": "+s.Err.Error(), ui.ToneBad))
		case !s.Enabled:
			rows = append(rows, ui.Text(label, "Off", ui.ToneMuted))
		default:
			rows = append(rows, ui.Text(label, "Not running", ui.ToneWarning))
		}
	}
	rows = append(rows, ui.Button("Restart outputs", ui.ButtonNormal, false, a.restartOutputs))

	rows = append(rows, ui.Section("Latency test"))
	soundLabel := "Play test sound"
	if a.cfg.TestSoundPath != "" {
		soundLabel = "Play “" + filepath.Base(a.cfg.TestSoundPath) + "”"
	}
	rows = append(rows,
		a.testActions(),
		ui.Button(soundLabel, ui.ButtonNormal, a.cfg.TestSoundPath == "", func() { a.playTest(false) }),
		ui.ClosingButton("Choose test sound…", ui.ButtonNormal, a.chooseTestSound),
	)
	rows = append(rows, a.resultRows()...)

	_, sounds := a.lib.Stats()
	rows = append(rows,
		ui.Section("Library index"),
		ui.Text("Sound index", fmt.Sprintf("%s · %s", plural(sounds, "sound"), audio.FormatBytes(a.lib.IndexSize())), ui.ToneNormal),
	)
	switch st := a.indexStatus(); {
	case st.Busy:
		rows = append(rows, ui.Text("Reading", fmt.Sprintf("%d / %d", st.Done, st.Total), ui.ToneWarning))
	case st.Failed > 0:
		rows = append(rows, ui.Text("Without length", plural(st.Failed, "sound"), ui.ToneMuted))
	}

	rows = append(rows, ui.Section("Phone server"))
	if a.srv == nil || a.serverErr != nil {
		rows = append(rows, ui.Text("Port", fmt.Sprintf("%d · not running", a.cfg.ServerPort), ui.ToneBad))
	} else {
		rows = append(rows,
			ui.Text("Port", fmt.Sprint(a.cfg.ServerPort), ui.ToneNormal),
			ui.Text("Paired devices", fmt.Sprint(a.srv.Auth().SessionCount()), ui.ToneNormal))
	}

	mode := "installed"
	if a.portable {
		mode = "portable"
	}
	rows = append(rows,
		ui.Section("Files"),
		ui.Text("Version", Version+" · "+mode, ui.ToneNormal),
		ui.Buttons(
			ui.Action{Label: "Open log", Closes: true, OnClick: func() { _ = ui.ShellOpen(filepath.Join(config.Dir(), "debug.log")) }},
			ui.Action{Label: "Data folder", Closes: true, OnClick: func() { _ = ui.ShellOpen(config.Dir()) }},
		),
	)
	if exe, err := os.Executable(); err == nil {
		rows = append(rows, ui.ClosingButton("Open program folder", ui.ButtonNormal, func() { _ = ui.ShellOpen(filepath.Dir(exe)) }))
	}
	return append(rows, ui.Spacer(4))
}

// resultRows report the last test play: decode time and, per output, the
// measured queue delay plus the device's reported latency.
func (a *App) resultRows() []ui.Row {
	pb := &a.playback
	var rows []ui.Row
	if pb.lastError != "" {
		rows = append(rows, ui.Paragraph(pb.lastError, ui.ToneBad))
	}
	if pb.last == nil {
		return append(rows, ui.Text("Play a test to measure the delay to each output.", "", ui.ToneMuted))
	}
	rows = append(rows, ui.Text("Last", fmt.Sprintf("%s · %.1fs", pb.last.Name, pb.last.Length.Seconds()), ui.ToneNormal))
	if pb.last.Decode > 0 {
		rows = append(rows, ui.Text("Ready in", fmt.Sprintf("%.1f ms", ms(pb.last.Decode)), ui.ToneNormal))
	}
	for _, s := range a.engine.Status() {
		if !s.Running {
			continue
		}
		d, ok := pb.delays[s.Role]
		if !ok {
			rows = append(rows, ui.Text(roleLabel(s.Role), "waiting…", ui.ToneMuted))
			continue
		}
		// Queue delay is measured; the device part is the stream latency
		// plus the buffer, an upper bound reported by WASAPI.
		device := s.Info.StreamLatency + s.Info.Buffer
		rows = append(rows, ui.Text(roleLabel(s.Role), fmt.Sprintf("%.1f + ≤%.0f ms", ms(d), ms(device)), ui.ToneGood))
	}
	if pb.mutedMic != "" {
		rows = append(rows, ui.Text("Mic", "muted while playing", ui.ToneWarning))
	}
	return rows
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
