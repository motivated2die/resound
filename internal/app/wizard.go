package app

import (
	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/install"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// The guided setup runs on first start (until config.SetupComplete) and
// from Settings. A welcome page is followed by five steps, each a plain
// page of rows shared with the settings pages; every step can be skipped
// and redone later.

const wizardSteps = 5

// wizardVoiceStep is the step that connects voice chat (VB-Cable).
const wizardVoiceStep = 3

func (a *App) startWizard() {
	a.wizardStep = 0
	a.showPage(pageWizard)
}

func (a *App) wizardGo(step int) {
	a.wizardStep = max(0, min(step, wizardSteps))
	a.removing = ""
	if !a.cfg.SetupComplete && a.cfg.SetupStep != a.wizardStep {
		a.cfg.SetupStep = a.wizardStep // continue here after a restart
		a.save()
	}
	a.showPage(pageWizard)
}

func (a *App) finishWizard() {
	if !a.cfg.SetupComplete || a.cfg.SetupStep != 0 {
		a.cfg.SetupComplete, a.cfg.SetupStep = true, 0
		a.save()
	}
	logx.Printf("guided setup: finished at step %d", a.wizardStep)
	a.showPage(pageHome)
}

func (a *App) wizardPage() []ui.Row {
	switch a.wizardStep {
	case 1:
		return a.wizardSounds()
	case 2:
		return a.wizardSpeakers()
	case wizardVoiceStep:
		return a.wizardVoice()
	case 4:
		return a.wizardKeys()
	case 5:
		return a.wizardPhone()
	}
	return a.wizardWelcome()
}

// wizardHeader is a step's header; back returns to the previous step.
func (a *App) wizardHeader(title string) ui.Row {
	step := a.wizardStep
	return ui.WizardHeader(title, step, wizardSteps, func() { a.wizardGo(step - 1) })
}

// wizardNext moves on: "Continue" once the step is done, "Skip for now"
// while it isn't.
func (a *App) wizardNext(done bool) ui.Row {
	next := func() { a.wizardGo(a.wizardStep + 1) }
	if done {
		return ui.Button("Continue", ui.ButtonPrimary, false, next)
	}
	return ui.Button("Skip for now", ui.ButtonNormal, false, next)
}

func (a *App) wizardWelcome() []ui.Row {
	return []ui.Row{
		ui.Brand(a.logo, "RESOUND", "", "Guided setup · about 2 minutes", nil, nil),
		ui.Paragraph("Welcome! Play sounds into games and voice chat, controlled from your phone. "+
			"You hear them on your speakers; the others hear them through your microphone.", ui.ToneNormal),
		ui.Paragraph("Five short steps: add your sounds, check your audio, connect voice chat, set your push-to-talk key and pair your phone.", ui.ToneMuted),
		ui.Flex(),
		ui.Button("Start setup", ui.ButtonPrimary, false, func() { a.wizardGo(1) }),
		ui.Button("Skip, I'll set it up myself", ui.ButtonNormal, false, a.finishWizard),
		ui.Button("Restore from a backup…", ui.ButtonNormal, false, func() { a.openPage(pageBackup) }),
		ui.Spacer(4),
	}
}

func (a *App) wizardSounds() []ui.Row {
	rows := []ui.Row{
		a.wizardHeader("Add your sounds"),
		ui.Paragraph("Drag folders with your sound files (MP3, WAV, FLAC or OGG) onto the field below, or choose them. "+
			"Each folder and each of its subfolders becomes a tab on your phone.", ui.ToneMuted),
	}
	rows = append(rows, a.folderRows()...)
	rows = append(rows, a.dropRows()...)
	rows = append(rows, a.libraryActions()...)
	if st := a.indexStatus(); st.Busy {
		rows = append(rows, ui.Text("Reading new sounds…", "", ui.ToneMuted))
	}
	return append(rows, ui.Flex(), ui.Separator(), a.wizardNext(len(a.cfg.Folders) > 0), ui.Spacer(4))
}

func (a *App) wizardSpeakers() []ui.Row {
	rows := []ui.Row{
		a.wizardHeader("Check your audio"),
		ui.Paragraph("Choose your microphone and where you hear sounds, then speak and play the test sound: "+
			"the bars show what arrives where.", ui.ToneMuted),
	}
	rows = append(rows, a.flowRows(true)...)
	running := false
	if s, ok := a.outputStatus(audio.RoleMonitor); ok && s.Running {
		running = true
	}
	return append(rows, ui.Flex(), ui.Separator(), a.wizardNext(running), ui.Spacer(4))
}

func (a *App) wizardVoice() []ui.Row {
	rows := []ui.Row{
		a.wizardHeader("Connect voice chat"),
		ui.Paragraph("Others hear your sounds through VB-Cable, a free virtual audio driver by VB-Audio. "+
			"Your microphone is passed through it as well, so they still hear you.", ui.ToneMuted),
	}
	if !a.route.CableInstalled() {
		rows = append(rows, ui.Section("Install VB-Cable"))
		rows = append(rows, a.cableInstallRows()...)
		rows = append(rows, a.cableCredit()...)
		return append(rows, ui.Flex(), ui.Separator(), a.wizardNext(false), ui.Spacer(4))
	}
	rows = append(rows, a.cableNoteRows()...)
	rows = append(rows, a.flowRows(false)...)
	rows = append(rows, a.connectRows()...)
	rows = append(rows, a.cableOutputHint()...)
	rows = append(rows, a.cableCredit()...)
	return append(rows, ui.Flex(), ui.Separator(), a.wizardNext(a.setupStepsDone() == 3), ui.Spacer(4))
}

// connectCable does steps 2 and 3 in one go: sounds into CABLE Input, then
// the microphone into the cable (through the elevated helper).
func (a *App) connectCable() {
	if !a.cableSelected() {
		a.useCableAsOutput()
	}
	if !a.route.MicRouted && a.route.Mic != nil {
		a.setMicRouting(true)
	}
}

func (a *App) wizardKeys() []ui.Row {
	rows := []ui.Row{a.wizardHeader("Push-to-talk key"), a.pttIntro()}
	if !a.ptt.capturing {
		rows = append(rows, ui.Paragraph("Pick a key no one types, such as F13, and bind the same key in your game: "+
			"use “Send it to the game”, then click the game's keybind field. No push-to-talk? Skip this step.", ui.ToneMuted))
	}
	rows = append(rows, a.pttRows()...)
	return append(rows, ui.Flex(), ui.Separator(), a.wizardNext(len(a.cfg.PushToTalkKeys) > 0), ui.Spacer(4))
}

func (a *App) wizardPhone() []ui.Row {
	rows := []ui.Row{
		a.wizardHeader("Connect your phone"),
		ui.Paragraph("Scan the code with your phone's camera. The Resound app opens and pairs; install it from there so it starts from your home screen.", ui.ToneMuted),
	}
	pair, ok := a.pairRows()
	rows = append(rows, pair...)
	if ok && a.srv.Auth().DeviceCount() > 0 {
		rows = append(rows, ui.Text("Connected", plural(a.srv.Auth().DeviceCount(), "device"), ui.ToneGood))
	}
	rows = append(rows, a.firewallRows()...)
	rows = append(rows, ui.Flex(), ui.Separator())
	if !a.portable { // dev builds never register themselves
		rows = append(rows, ui.Toggle("Start Resound with Windows", install.AutoStartEnabled(), a.toggleAutoStart))
	}
	return append(rows,
		ui.Button("Finish setup", ui.ButtonPrimary, false, a.finishWizard),
		ui.Text("Pair more phones any time from the panel.", "", ui.ToneMuted),
		ui.Spacer(4),
	)
}
