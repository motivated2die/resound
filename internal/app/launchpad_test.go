package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/library"
)

// An app can have a launchpad key of its own or none, like the overlay's;
// the launchpad key is one job, also against the overlay's.
func TestAppLaunchpadKey(t *testing.T) {
	const vkCaps = 0x76 // F7: named alike in every keyboard language
	a := keybindsApp()
	a.cfg.OverlayKeys = []uint16{vkF8}
	a.cfg.LaunchpadKeys = []uint16{vkCaps}
	launchpad := func(app string) string { k := a.keymap(); return k.For(app).Launchpad.String() }

	if got := launchpad("game.exe"); got != "F7" {
		t.Errorf("game keeps the global key: %q", got)
	}
	a.appKeybinds("plain.exe").Launchpad = &config.KeyOverride{Keys: [][]uint16{{vkF10}}}
	if got := launchpad("plain.exe"); got != "F10" {
		t.Errorf("plain's own key: %q", got)
	}
	a.appKeybinds("game.exe").Launchpad = &config.KeyOverride{Off: true}
	if got := launchpad("game.exe"); got != "" {
		t.Errorf("game turned it off: %q", got)
	}
	if got := overrideSummary(a.appKeybinds("game.exe")); !strings.Contains(got, "launchpad") {
		t.Errorf("game's summary: %q", got)
	}
	if exes := a.appsWithOwn(fnLaunchpad); len(exes) != 2 {
		t.Errorf("apps with their own launchpad setting: %v", exes)
	}

	if got := a.keyConflict(keyTarget{"", fnLaunchpad}, input.Combo{vkF8}); !strings.Contains(got, "the overlay key already") {
		t.Errorf("the overlay key as the launchpad's: %q", got)
	}
	if got := a.keyConflict(keyTarget{"", fnOverlay}, input.Combo{vkCaps}); !strings.Contains(got, "the launchpad key") {
		t.Errorf("the launchpad key as the overlay's: %q", got)
	}
	if got := a.keyConflict(keyTarget{"", fnLaunchpad}, input.Combo{vkF9}); got != "" {
		t.Errorf("another launchpad key (replaces it): %q", got)
	}
	if got := a.keyConflict(keyTarget{"plain.exe", fnPTT}, input.Combo{vkF10}); !strings.Contains(got, "the launchpad key in Plain already") {
		t.Errorf("plain's launchpad key as its push-to-talk: %q", got)
	}

	a.cfg.LaunchpadKeys = nil
	if detail, _ := a.launchpadSummary(); detail != "In some apps" {
		t.Errorf("summary with only plain's key: %q", detail)
	}
}

// launchpadApp has a library of two tabs: "Board" with 40 sounds, s01…s40
// (shown A–Z on the phone), and its subfolder "More" with two.
func launchpadApp(t *testing.T) *App {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Board")
	for _, dir := range []string{root, filepath.Join(root, "More")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 40; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("s%02d.mp3", i)), []byte("x"), 0o644)
	}
	for _, name := range []string{"m1.mp3", "m2.mp3"} {
		os.WriteFile(filepath.Join(root, "More", name), []byte("x"), 0o644)
	}
	lib := library.Open(t.TempDir(), audio.SupportedExtensions)
	t.Cleanup(func() { _ = lib.Close() })
	lib.Scan([]string{root})
	a := &App{cfg: config.Config{Folders: []string{root}, NoLaunchpadBadge: true}, lib: lib}
	tabs := lib.PhoneTabs()
	if len(tabs) != 2 || len(tabs[0].Sounds) != 40 {
		t.Fatalf("library: %d tabs", len(tabs))
	}
	if _, err := lib.UpdateLayout(strconv.FormatUint(tabs[0].ID, 10), func(l *library.Layout) { l.Sort = library.SortNameAsc }); err != nil {
		t.Fatal(err)
	}
	return a
}

// The keys go to the buttons as the phone shows them: 1–9, 0, then A–Z,
// 36 at most.
func TestLaunchpadKeys(t *testing.T) {
	a := launchpadApp(t)
	board := a.lib.PhoneTabs()[0]
	a.launchpad.tab = strconv.FormatUint(board.ID, 10)
	a.startLaunchpad()
	if !a.launchpad.held || len(a.launchpad.keys) != len(launchpadKeys) {
		t.Fatalf("held %v with %d keys, want %d", a.launchpad.held, len(a.launchpad.keys), len(launchpadKeys))
	}
	name := func(key byte) string {
		s, _, _ := a.lib.Sound(a.launchpad.keys[uint16(key)])
		return s.Name
	}
	for key, want := range map[byte]string{'1': "s01", '9': "s09", '0': "s10", 'A': "s11", 'Z': "s36"} {
		if got := name(key); got != want {
			t.Errorf("key %c plays %q, want %q", key, got, want)
		}
	}
	a.launchpadPlay('!') // no such key: nothing happens
	a.endLaunchpad()
	if a.launchpad.held || a.launchpad.keys != nil || a.launchpad.tab != strconv.FormatUint(board.ID, 10) {
		t.Fatalf("after letting go: held %v, keys %v, tab %q (the tab must stay)", a.launchpad.held, a.launchpad.keys, a.launchpad.tab)
	}
}

// The arrows and the wheel step through the phone's tabs, Favorites first,
// and stop at the ends; a tab that's gone falls back to Favorites.
func TestLaunchpadStep(t *testing.T) {
	a := launchpadApp(t)
	tabs := a.lib.PhoneTabs()
	board, more := strconv.FormatUint(tabs[0].ID, 10), strconv.FormatUint(tabs[1].ID, 10)
	a.launchpad.tab = "12345" // a tab that no longer exists
	a.startLaunchpad()
	if a.launchpad.tab != library.FavoritesKey || len(a.launchpad.keys) != 0 {
		t.Fatalf("gone tab: on %q with %d keys, want Favorites (none)", a.launchpad.tab, len(a.launchpad.keys))
	}
	for _, s := range []struct {
		steps int
		want  string
	}{{-1, library.FavoritesKey}, {1, board}, {1, more}, {5, more}, {-2, library.FavoritesKey}, {2, more}} {
		a.launchpadStep(s.steps)
		if a.launchpad.tab != s.want {
			t.Fatalf("step %+d: on %q, want %q", s.steps, a.launchpad.tab, s.want)
		}
	}
	if len(a.launchpad.keys) != 2 || a.tabName(more) != "More" {
		t.Fatalf("on More: %d keys, named %q", len(a.launchpad.keys), a.tabName(more))
	}
}

// Space stops all sounds when pressed; with LaunchpadSpacePause, a tap
// pauses or resumes them, and holding it (the timer firing) stops them. A
// tap's release may come after the launchpad ended.
func TestLaunchpadSpace(t *testing.T) {
	a := launchpadApp(t)
	a.engine = audio.NewEngine() // no outputs: nothing plays
	a.playback.init()
	a.ptt.init()
	var timers []func()
	old := afterUI
	afterUI = func(_ *App, _ time.Duration, fn func()) { timers = append(timers, fn) }
	t.Cleanup(func() { afterUI = old })
	fire := func() {
		for _, fn := range timers {
			fn()
		}
		timers = nil
	}
	space := func(down bool) { a.onLaunchpadEvent(input.HotkeyEvent{Action: input.ActionLaunchpadSpace, Down: down}) }
	paused := func() { a.playback.pausedActive, a.playback.paused = true, []uint64{7} }

	a.startLaunchpad()
	paused()
	space(true)
	if a.playback.pausedActive || len(timers) != 0 {
		t.Fatalf("a press stops all: paused %v, %d timers", a.playback.pausedActive, len(timers))
	}
	space(false)

	a.cfg.LaunchpadSpacePause = true
	space(true)
	space(false)
	if a.playback.pausedActive {
		t.Fatal("a tap with nothing playing paused nothing, and must not say paused")
	}
	fire() // the tap's timer, too late
	paused()
	space(true)
	space(false)
	if a.playback.pausedActive {
		t.Fatal("a tap resumes")
	}
	fire()

	paused()
	space(true)
	fire() // held long enough
	if a.playback.pausedActive || a.launchpad.space != 0 {
		t.Fatalf("held: paused %v, timing %d", a.playback.pausedActive, a.launchpad.space)
	}
	paused()
	space(false) // the long press's release
	if !a.playback.pausedActive {
		t.Fatal("the release after a long press toggled the pause")
	}

	space(true)
	a.endLaunchpad()
	space(false)
	if a.playback.pausedActive {
		t.Fatal("a tap let go after the launchpad ended still resumes")
	}
	fire()
	paused()
	space(true) // not held: nothing
	space(false)
	if !a.playback.pausedActive || len(timers) != 0 {
		t.Fatal("Space without the launchpad did something")
	}
}
