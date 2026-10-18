package app

// "Mute my microphone while sounds play" and its threshold: only sounds
// longer than it mute the mic (playback.go, muteFor), shorter ones leave it
// open. The threshold steps with − and +, or takes a typed value.

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/motivated2die/resound/internal/ui"
)

// muteSteps are the thresholds − and + step through, in seconds; 0 reads
// "Always".
var muteSteps = []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 12, 15, 20, 25, 30, 45, 60, 90, 120, 180, 300, 600}

// maxMuteSeconds is the longest threshold that can be typed.
const maxMuteSeconds = 600

// muteStepDown and muteStepUp return the step below and above v (which may
// be a typed value between steps), or v at either end.
func muteStepDown(v float64) float64 {
	for i := len(muteSteps) - 1; i >= 0; i-- {
		if muteSteps[i] < v {
			return muteSteps[i]
		}
	}
	return v
}

func muteStepUp(v float64) float64 {
	for _, s := range muteSteps {
		if s > v {
			return s
		}
	}
	return v
}

// formatMuteSeconds is a threshold as the panel shows it: "Always", "5 s",
// "7.5 s".
func formatMuteSeconds(v float64) string {
	if v <= 0 {
		return "Always"
	}
	return strconv.FormatFloat(v, 'f', -1, 64) + " s"
}

// parseMuteSeconds reads a typed threshold: "7", "7.5", "7,5" or "7 s".
// Under a second means "Always" (0); values are rounded to a tenth and
// capped at maxMuteSeconds.
func parseMuteSeconds(text string) (float64, error) {
	t := strings.ToLower(strings.TrimSpace(text))
	for _, unit := range []string{"seconds", "second", "sec", "s"} {
		if strings.HasSuffix(t, unit) {
			t = strings.TrimSpace(strings.TrimSuffix(t, unit))
			break
		}
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(t, ",", "."), 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("type a number of seconds, such as 5 or 2.5")
	}
	v = math.Round(min(v, maxMuteSeconds)*10) / 10
	if v < 1 {
		v = 0
	}
	return v, nil
}

func (a *App) toggleMuteWhilePlaying() {
	a.cfg.MuteMicWhilePlaying = !a.cfg.MuteMicWhilePlaying
	a.muteEdit, a.muteEditErr = false, ""
	a.syncMic() // a long sound playing right now mutes the mic at once
	a.save()
	a.render()
}

// setMuteSeconds sets the threshold; a sound playing now is judged again.
func (a *App) setMuteSeconds(v float64) {
	a.cfg.MuteMicMinSeconds = v
	a.muteEdit, a.muteEditErr = false, ""
	a.syncMic()
	a.save()
	a.render()
}

func (a *App) typeMuteSeconds(text string) {
	v, err := parseMuteSeconds(text)
	if err != nil {
		a.muteEditErr = err.Error()
		a.render()
		return
	}
	a.setMuteSeconds(v)
}

// muteMicRows are the option to mute the microphone while sounds play and,
// while it is on, its threshold right below it.
func (a *App) muteMicRows() []ui.Row {
	rows := []ui.Row{ui.Toggle("Mute my microphone while sounds play", a.cfg.MuteMicWhilePlaying, a.toggleMuteWhilePlaying)}
	if !a.cfg.MuteMicWhilePlaying {
		return append(rows, ui.Paragraph("Resound can mute your mic while sounds play, so others don't hear you talking over them. It's back as soon as they end.", ui.ToneMuted))
	}
	v := a.cfg.MuteMicMinSeconds
	rows = append(rows, ui.StepperEdit("Minimum sound length", formatMuteSeconds(v),
		func() { a.setMuteSeconds(muteStepDown(v)) },
		func() { a.setMuteSeconds(muteStepUp(v)) },
		func() { a.muteEdit, a.muteEditErr = !a.muteEdit, ""; a.render() }))
	if a.muteEdit {
		const fieldID = "mute-min-seconds"
		value := ""
		if v > 0 {
			value = strconv.FormatFloat(v, 'f', -1, 64)
		}
		rows = append(rows, ui.TextField(fieldID, value, 8, a.typeMuteSeconds,
			func() { a.muteEdit, a.muteEditErr = false, ""; a.render() }))
		if a.muteEditErr != "" {
			rows = append(rows, ui.Paragraph(a.muteEditErr, ui.ToneBad))
		}
		rows = append(rows, ui.Buttons(
			ui.Action{Label: "Cancel", OnClick: func() { a.muteEdit, a.muteEditErr = false, ""; a.render() }},
			ui.Action{Label: "Set", Style: ui.ButtonPrimary, OnClick: func() {
				if text, ok := a.tray.Popup().FieldText(fieldID); ok {
					a.typeMuteSeconds(text)
				}
			}},
		))
	}
	text := "Every sound mutes your mic while it plays; it's back as soon as they end."
	if v > 0 {
		text = fmt.Sprintf("Sounds longer than %s mute your mic while they play. Shorter ones leave it open, so you can keep talking over quick effects. Below 1 s, every sound mutes it.", formatMuteSeconds(v))
	}
	return append(rows, ui.Paragraph(text, ui.ToneMuted))
}
