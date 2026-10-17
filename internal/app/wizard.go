package app

import (
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/install"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// The guided setup runs on first start (until config.SetupComplete) and
// from Settings. A welcome page is followed by four steps, each a plain
// page of rows shared with the settings pages; every step can be skipped
// and redone later: sounds, audio and voice chat, push-to-talk, phone.

const wizardSteps = 4

// wizardAudioStep is the step with the audio flow: devices, VB-Cable and
// routing the microphone into it.
const wizardAudioStep = 2

// setupFlow is the numbering of the setup's steps that config.SetupStep
// uses. Flow 0 had five steps: speakers (2) and voice chat (3) were apart.
const setupFlow = 1

// migrateSetupStep moves a setup in progress from the five-step numbering
// to the current one, once: the old speakers and voice chat steps are both
// the audio step now, and the steps after them move up by one.
func (a *App) migrateSetupStep() {
	if migrateSetupStep(&a.cfg) {
		a.save()
	}
}

// migrateSetupStep renumbers c's setup step; it reports whether c changed.
func migrateSetupStep(c *config.Config) bool {
	if c.SetupFlow >= setupFlow {
		return false
	}
	if !c.SetupComplete && c.SetupStep >= 3 {
		c.SetupStep--
	}
	c.SetupFlow = setupFlow
	return true
}

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
	case wizardAudioStep:
		return a.wizardAudio()
	case 3:
		return a.wizardKeys()
	case 4:
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
		ui.Paragraph("Four short steps: add your sounds, set up your audio and voice chat, set your push-to-talk key and pair your phone.", ui.ToneMuted),
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

// wizardAudio sets up audio and voice chat in one step around the audio
// flow: choose the devices and try them, install VB-Cable if it's missing,
// and route the microphone into it. Each problem shows on its pipe, and
// clicking its badge fixes it right here.
func (a *App) wizardAudio() []ui.Row {
	combined := "CABLE Output is the combined mic and effects."
	if a.ownRouting() {
		// Set up under Advanced in Settings; the setup only shows it.
		combined = "Your own routing combines your mic and the sounds (Settings → Audio & voice chat → Advanced)."
	}
	rows := []ui.Row{
		a.wizardHeader("Audio and voice chat"),
		ui.Paragraph("Choose your microphone and where you want to hear sounds yourself. Below is a live visualization. "+combined, ui.ToneMuted),
	}
	rows = append(rows, a.cableNoteRows()...)
	rows = append(rows, a.flowRows()...)
	if a.ownRouting() {
		rows = append(rows, a.cableOutputHint()...)
	} else if !a.route.CableInstalled() {
		rows = append(rows, ui.Section("Install VB-Cable"))
		rows = append(rows, a.cableInstallRows()...)
	} else {
		rows = append(rows, a.connectRows()...)
		rows = append(rows, a.cableOutputHint()...)
	}
	// VB-Audio's credit and donation links are on Settings → Audio & voice
	// chat; the intro above names them as VB-Cable's maker.
	rows = append(rows, ui.Spacer(4))
	rows = append(rows, a.muteMicRows()...)
	return append(rows, ui.Flex(), ui.Separator(), a.wizardNext(a.voiceReady()), ui.Spacer(4))
}

// connectCable connects voice chat in one go: sounds into CABLE Input (the
// automatic choice, unless another device was chosen), then the microphone
// into the cable (through the elevated helper).
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
		rows = append(rows, ui.Paragraph("TIP: pick a rarely used key, such as Pause or a numpad key, and bind it as a secondary voice activation key. Or just bind your primary PTT key here. "+
			"Use “Send it to the game”, then trigger the game's keybind field. No push-to-talk? Skip this step.", ui.ToneMuted))
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
