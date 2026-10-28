package app

import (
	"slices"
	"strconv"
	"strings"

	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/server"
	"github.com/motivated2die/resound/internal/ui"
)

// The launchpad: while its key is held (input.Filter takes the keyboard
// then), the phone in use (server.ActiveView) labels the buttons of the tab
// it shows with keys, 1–9, 0, then A–Z, in the order they stand on it
// (library.ShownSounds), and pressing one plays that sound as a tap would.
// Space stops all sounds (or, as set, a tap pauses or resumes them and a
// long press stops them); the arrow keys and the wheel switch tabs, on the
// phone too. Letting go of the key takes the labels away. Meanwhile a round
// badge shows on the screen (ui.LaunchpadBadge), with the tab's name and,
// for a moment, a sound played from it; each of them can be turned off.
//
// The PC decides which key plays which sound and sends the phone the
// labels by sound ID, so a label always names the key that plays its
// button, whatever the phone's screen, layout or sorting. Without a phone,
// the keys play the tab shown last (Favorites at first). The launchpad is
// on while a key is set for it: the global one, or an app's own while that
// app is in the foreground (keybinds.go), which can also turn it off there.

// launchpadKeys label the buttons, in order: the keyboard's top row, then
// the letters.
const launchpadKeys = "1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// launchpadSpaceHold is how long Space is held to stop all sounds when a
// tap pauses (LaunchpadSpacePause); as long as Esc in the overlay.
const launchpadSpaceHold = input.EscHoldTime

// launchpadState is the launchpad's UI-thread state.
type launchpadState struct {
	held   bool
	conn   uint64            // the remote it shows on; 0 = none
	tab    string            // the tab its keys play; while not held, the one shown last
	keys   map[uint16]uint64 // key ('1'…'Z') → sound
	sent   string            // what conn was sent last (launchpadSig)
	badge  *ui.LaunchpadBadge
	err    string // why the badge can't be shown, if it can't
	space  uint64 // the Space press being timed (LaunchpadSpacePause); 0 = none
	spaces uint64 // Space presses so far, to tell them apart
}

// onLaunchpadEvent handles the launchpad's hotkey events; it reports false
// for other events. UI thread.
func (a *App) onLaunchpadEvent(ev input.HotkeyEvent) bool {
	switch ev.Action {
	case input.ActionLaunchpad:
		if ev.Down {
			a.startLaunchpad()
		} else {
			a.endLaunchpad()
		}
	case input.ActionLaunchpadKey:
		a.launchpadPlay(ev.Key)
	case input.ActionLaunchpadSpace:
		a.launchpadSpace(ev.Down)
	case input.ActionLaunchpadTab:
		a.launchpadStep(ev.Steps)
	default:
		return false
	}
	return true
}

func (a *App) startLaunchpad() {
	lp := &a.launchpad
	lp.held, lp.conn, lp.sent = true, 0, ""
	if v, ok := a.activeView(); ok {
		lp.conn = v.Conn
		if v.Tab != "" {
			lp.tab = v.Tab
		}
	}
	logx.Printf("launchpad: on (remote %d)", lp.conn)
	a.showLaunchpad()
}

func (a *App) endLaunchpad() {
	lp := &a.launchpad
	if !lp.held {
		return
	}
	lp.held = false
	if lp.conn != 0 && a.srv != nil {
		a.srv.SendLaunchpad(lp.conn, server.Launchpad{Tab: lp.tab})
	}
	lp.conn, lp.sent, lp.keys = 0, "", nil
	if lp.badge != nil {
		lp.badge.Hide()
	}
	logx.Printf("launchpad: off")
}

// launchpadSpace handles Space going down or up. A press stops all sounds;
// with LaunchpadSpacePause, a tap pauses or resumes them instead, and
// holding it for launchpadSpaceHold stops them. Its release may come after
// the launchpad ended: a tap begun while it was held still counts.
func (a *App) launchpadSpace(down bool) {
	lp := &a.launchpad
	switch {
	case down && !lp.held:
	case down && !a.cfg.LaunchpadSpacePause:
		a.launchpadStopAll()
	case down:
		lp.spaces++
		press := lp.spaces
		lp.space = press
		afterUI(a, launchpadSpaceHold, func() {
			if lp.space == press { // still held
				lp.space = 0
				a.launchpadStopAll()
			}
		})
	case lp.space != 0: // let go in time: a tap
		lp.space = 0
		a.launchpadPause()
	}
}

func (a *App) launchpadStopAll() {
	logx.Printf("launchpad: stopping all sounds")
	a.reclaimPTT()
	a.stopAll()
	a.launchpadNote("", "All sounds stopped", true)
}

// launchpadPause pauses or resumes all sounds, like the phone's pause
// button (tapPause).
func (a *App) launchpadPause() {
	paused, ok := a.tapPause()
	if !ok {
		a.launchpadNote("", "Nothing is playing", false)
		return
	}
	logx.Printf("launchpad: paused=%v", paused)
	a.render() // the audio flow shows the pause
	if paused {
		a.launchpadNote("", "Paused", true)
	} else {
		a.launchpadNote("", "Resumed", true)
	}
}

// activeView is the remote in use, if any.
func (a *App) activeView() (server.View, bool) {
	if a.srv == nil {
		return server.View{}, false
	}
	return a.srv.ActiveView()
}

// showLaunchpad gives the keys to the sounds of lp.tab (Favorites when it
// is gone), sends the remote in use their labels unless it has them, and
// shows the badge.
func (a *App) showLaunchpad() {
	lp := &a.launchpad
	sounds, ok := a.lib.ShownSounds(lp.tab)
	if !ok {
		lp.tab = library.FavoritesKey
		sounds, _ = a.lib.ShownSounds(lp.tab)
	}
	lp.keys = map[uint16]uint64{}
	labels := map[uint64]string{}
	sig := []string{strconv.FormatUint(lp.conn, 10), lp.tab}
	for i, s := range sounds[:min(len(sounds), len(launchpadKeys))] {
		k := launchpadKeys[i]
		lp.keys[uint16(k)] = s.ID
		labels[s.ID] = string(k)
		sig = append(sig, strconv.FormatUint(s.ID, 10))
	}
	if s := strings.Join(sig, " "); lp.conn != 0 && a.srv != nil && s != lp.sent {
		if a.srv.SendLaunchpad(lp.conn, server.Launchpad{On: true, Tab: lp.tab, Keys: labels}) {
			lp.sent = s
		}
	}
	a.showBadge()
}

// onRemoteView follows the remote in use while the launchpad is held: its
// tab, or another remote taking over. While not held, it only remembers the
// tab, for using the launchpad without a phone later. UI thread.
func (a *App) onRemoteView() {
	lp := &a.launchpad
	v, ok := a.activeView()
	if !lp.held {
		if ok && v.Tab != "" {
			lp.tab = v.Tab
		}
		return
	}
	if v.Conn != lp.conn {
		if lp.conn != 0 && a.srv != nil { // the labels move to the other remote
			a.srv.SendLaunchpad(lp.conn, server.Launchpad{Tab: lp.tab})
		}
		lp.conn, lp.sent = v.Conn, ""
		logx.Printf("launchpad: remote %d in use", lp.conn)
	}
	if ok && v.Tab != "" {
		lp.tab = v.Tab
	}
	a.showLaunchpad()
}

// launchpadPlay plays the sound of key, as a tap on the phone would.
func (a *App) launchpadPlay(key uint16) {
	lp := &a.launchpad
	if !lp.held {
		return
	}
	id, ok := lp.keys[key]
	if !ok {
		a.launchpadNote("", "Nothing on "+string(rune(key)), false)
		return
	}
	snd, _, _ := a.lib.Sound(id)
	go func() { // decodes off the UI thread, like a play from the phone
		started, err := a.playSound(id, false)
		if err != nil {
			logx.Printf("launchpad: playing sound %d: %v", id, err)
		} else if a.srv != nil {
			a.srv.Played(id, started)
		}
		a.tray.Post(func() { a.launchpadNote(snd.Emoji, snd.Name, err == nil) })
	}()
}

// launchpadNote shows on the badge what a key did, flashing green or red:
// the text only where it shows played sounds.
func (a *App) launchpadNote(emoji, text string, ok bool) {
	b := a.launchpad.badge
	if b == nil || !a.launchpad.held || a.cfg.NoLaunchpadBadge {
		return
	}
	if a.cfg.NoLaunchpadSoundName {
		emoji, text = "", ""
	}
	b.Played(emoji, text, ok)
}

// launchpadStep switches tabs by steps in the phone's order (Favorites
// first), stopping at the ends.
func (a *App) launchpadStep(steps int) {
	lp := &a.launchpad
	if !lp.held {
		return
	}
	tabs := []string{library.FavoritesKey}
	for _, t := range a.lib.PhoneTabs() {
		tabs = append(tabs, strconv.FormatUint(t.ID, 10))
	}
	i := max(0, slices.Index(tabs, lp.tab))
	if j := max(0, min(len(tabs)-1, i+steps)); tabs[j] != lp.tab {
		lp.tab = tabs[j]
		a.showLaunchpad()
	}
}

// showBadge shows the badge as the settings say while the launchpad is
// held, with the tab's name, and hides it otherwise.
func (a *App) showBadge() {
	lp := &a.launchpad
	if !lp.held || a.cfg.NoLaunchpadBadge {
		if lp.badge != nil {
			lp.badge.Hide()
		}
		return
	}
	if lp.badge == nil {
		b, err := ui.NewLaunchpadBadge()
		if err != nil {
			logx.Printf("launchpad: %v", err)
			lp.err = "The badge can't be shown on this PC: " + err.Error()
			return
		}
		lp.badge, lp.err = b, ""
	}
	label := ""
	if !a.cfg.NoLaunchpadTabName {
		label = a.tabName(lp.tab)
	}
	lp.badge.Show(ui.ParseBadgeAt(a.cfg.LaunchpadBadgeAt), label)
}

// tabName is what the phone calls a tab.
func (a *App) tabName(key string) string {
	if key == library.FavoritesKey {
		return "Favorites"
	}
	if id, err := strconv.ParseUint(key, 10, 64); err == nil {
		if t, ok := a.lib.Tab(id); ok {
			return t.Name
		}
	}
	return ""
}

// --- panel ------------------------------------------------------------------

func (a *App) launchpadSummary() (string, ui.Tone) {
	switch {
	case len(a.cfg.LaunchpadKeys) > 0:
		return "Hold " + input.Combo(a.cfg.LaunchpadKeys).String(), ui.ToneGood
	case a.keyUsed(fnLaunchpad):
		return "In some apps", ui.ToneGood
	}
	return "Off", ui.ToneMuted
}

func (a *App) launchpadPage() []ui.Row {
	// Beside a small picture of three buttons with their keys, like the
	// overlay's page.
	how, key := "Hold a key in any game and your phone's buttons show numbers. Press one to play that sound.", ""
	if len(a.cfg.LaunchpadKeys) > 0 {
		key = input.Combo(a.cfg.LaunchpadKeys).String()
		how = "Hold " + key + " in a game: your phone shows a key on each button, 1 to 9, 0, then A to Z. Press one to play that sound."
	}
	rows := []ui.Row{
		ui.HeaderButton("Launchpad", a.back, ui.IconHelp, "Help", "How the launchpad works: opens the help article", func() { a.openHelpAt(helpLaunchpad) }),
		ui.LaunchpadPreview(key, how,
			"The arrow keys or the wheel switch tabs, Space stops or pauses sounds (below). Your game gets none of these keys meanwhile."),
		ui.Section("Key"),
		a.keybindCard(keyTarget{fn: fnLaunchpad}),
		a.appKeysNote(fnLaunchpad, "A game can have a launchpad key of its own, or no launchpad at all."),
		ui.Section("Space"),
		// Greyed out while the launchpad is off (no key anywhere), like the
		// overlay's screen edge.
		ui.Segmented([]string{"Space to stop", "Space to pause"}, map[bool]int{false: 0, true: 1}[a.cfg.LaunchpadSpacePause],
			!a.keyUsed(fnLaunchpad), func(i int) { a.setSpacePause(i == 1) }),
		ui.Paragraph(a.spaceHelp(), ui.ToneMuted),
		ui.Section("On-screen badge"),
		ui.ToggleInfo("Show a badge while you hold the key",
			"A round yellow badge on your screen says the launchpad is on, without looking at your phone.",
			!a.cfg.NoLaunchpadBadge, func() { a.setLaunchpadOption(&a.cfg.NoLaunchpadBadge) }),
	}
	if !a.cfg.NoLaunchpadBadge {
		rows = append(rows,
			ui.ToggleInfo("Show the tab's name",
				"The tab your keys play, beside the badge. It changes as you switch tabs.",
				!a.cfg.NoLaunchpadTabName, func() { a.setLaunchpadOption(&a.cfg.NoLaunchpadTabName) }),
			ui.ToggleInfo("Show the sounds you play",
				"Each sound's emoji and name, for a moment, when you press its key.",
				!a.cfg.NoLaunchpadSoundName, func() { a.setLaunchpadOption(&a.cfg.NoLaunchpadSoundName) }),
			ui.Section("Position on screen"),
			ui.ScreenSpots(ui.ParseBadgeAt(a.cfg.LaunchpadBadgeAt), !a.keyUsed(fnLaunchpad), a.setBadgeAt),
		)
	}
	if a.launchpad.err != "" {
		rows = append(rows, ui.Paragraph(a.launchpad.err, ui.ToneBad))
	}
	return append(rows, ui.Spacer(4))
}

// spaceHelp explains the Space mode chosen.
func (a *App) spaceHelp() string {
	if a.cfg.LaunchpadSpacePause {
		return "Tap Space to pause and unpause sounds. Hold Space to stop them."
	}
	return "Tap Space to stop all playing sounds."
}

func (a *App) setSpacePause(on bool) {
	if on == a.cfg.LaunchpadSpacePause {
		return
	}
	a.cfg.LaunchpadSpacePause = on
	a.save()
	a.render()
}

// setLaunchpadOption flips one of the badge's switches (the No… settings).
func (a *App) setLaunchpadOption(off *bool) {
	*off = !*off
	a.save()
	a.showBadge()
	a.render()
}

func (a *App) setBadgeAt(at ui.BadgeAt) {
	a.cfg.LaunchpadBadgeAt = at.String()
	if at == ui.BadgeTopCenter {
		a.cfg.LaunchpadBadgeAt = "" // the default
	}
	a.save()
	a.showBadge()
	a.render()
}
