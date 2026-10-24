//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetWindow           = user32.NewProc("GetWindow")
)

const gwHwndNext = 2

// screens lists every monitor's full rectangle.
func screens() []rect {
	var out []rect
	cb := windows.NewCallback(func(mon, dc uintptr, r *rect, data uintptr) uintptr {
		out = append(out, *r)
		return 1
	})
	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}

func windowRect(hwnd uintptr) rect {
	var r rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func inside(r, outer rect) bool {
	return r.Left >= outer.Left && r.Top >= outer.Top && r.Right <= outer.Right && r.Bottom <= outer.Bottom
}

// TestPanelMoveLive shows the real panel and moves it like a drag by its
// tab: past every screen edge, onto each screen, back with Esc, to a
// screen that no longer exists, and docked again. The panel and its tab
// must always lie inside one screen's work area, with the tab and shadow
// following. It also sends the mouse's back button. It doesn't touch the
// running Resound or its config.
//
//	$env:RESOUND_PANEL_LIVE="1"; go test -run PanelMoveLive -v ./internal/ui
func TestPanelMoveLive(t *testing.T) {
	if os.Getenv("RESOUND_PANEL_LIVE") != "1" {
		t.Skip("set RESOUND_PANEL_LIVE=1 to show and move the panel on the screen")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	InitDPIAwareness()
	p, err := newPopup()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Hide()
	backs := 0
	short := []Row{Header("Test", func() { backs++ }), Text("Row", "value", ToneNormal)}
	tall := []Row{Header("Test", func() { backs++ })}
	for range 40 {
		tall = append(tall, Text("Row", "value", ToneNormal))
	}
	p.SetRows(short)
	var moved []*PanelPos
	p.OnMove = func(pos *PanelPos) { moved = append(moved, pos) }

	primary, _ := monitorAt(point{0, 0})
	icon := point{primary.Right - 40, primary.Bottom + 20} // where a tray icon sits
	p.dockAnchor = func() point { return icon }

	// check verifies the panel, its tab and its shadow against each other
	// and that the panel and tab lie inside one work area.
	check := func(step string) rect {
		t.Helper()
		pump(30e6)
		r := windowRect(p.hwnd)
		tab := windowRect(p.tab.hwnd)
		if tab.Left != r.Left || tab.Right != r.Right || tab.Bottom != r.Top+p.tab.h-p.tab.visibleH(p.canvas(0)) {
			t.Errorf("%s: tab %v doesn't sit above panel %v", step, tab, r)
		}
		if sh := windowRect(p.shadow.hwnd); sh.Left != r.Left-p.shadow.pad || sh.Top != r.Top-p.shadow.pad {
			t.Errorf("%s: shadow %v doesn't follow panel %v", step, sh, r)
		}
		whole := rect{r.Left, tab.Top, r.Right, r.Bottom}
		if work, _, ok := monitorInfoAt(point{r.Left, r.Top}); !ok || !inside(whole, work) {
			t.Errorf("%s: panel and tab %v not inside the work area %v", step, whole, work)
		}
		return r
	}
	drag := func(from, to point) {
		p.beginMove(from)
		p.moveTo(point{(from.X + to.X) / 2, (from.Y + to.Y) / 2})
		p.moveTo(to)
	}
	tabPoint := func() point {
		r := windowRect(p.hwnd)
		return point{r.Left + 30, r.Top - 8}
	}

	p.ShowAt(icon)
	r := check("docked")
	if dir := os.Getenv("RESOUND_RENDER_DIR"); dir != "" {
		pad := p.canvas(0).px(24)
		saveScreen(t, rect{r.Left - pad, r.Top - p.tab.h - pad, r.Right + pad, r.Top + p.canvas(0).px(80)}, filepath.Join(dir, "live-panel-top.png"))
	}

	// Raising the panel (a click activates it) brings its tab and shadow
	// along: no other always-on-top window can come between them.
	other, err := createWindow(wsExToolWindow|wsExTopmost|wsExNoActivate, wsPopup, shadowClass)
	if err != nil {
		t.Fatal(err)
	}
	defer procDestroyWindow.Call(other)
	procSetWindowPos.Call(other, hwndTopmost, uintptr(r.Left), uintptr(r.Top-200), 100, 400, swpNoActivate|swpShowWindow)
	procSetWindowPos.Call(p.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	pump(30e6)
	if next, _, _ := procGetWindow.Call(p.hwnd, gwHwndNext); next != p.tab.hwnd {
		t.Errorf("raised panel: the window below it is %#x, not its tab %#x", next, p.tab.hwnd)
	}
	if next, _, _ := procGetWindow.Call(p.tab.hwnd, gwHwndNext); next != p.shadow.hwnd {
		t.Errorf("raised panel: the window below its tab is %#x, not its shadow %#x", next, p.shadow.hwnd)
	}
	procShowWindow.Call(other, swHide)
	c := p.canvas(0)
	if want := dockedPos(icon, primary, p.width, p.height, c.px(popupGapL), p.tab.visibleH(c)); r.Left != want.X || r.Top != want.Y {
		t.Errorf("docked at %v, want %v", r, want)
	}

	// Past each edge of the primary screen: stopped at its work area.
	center := point{primary.Left + primary.w()/2, primary.Top + primary.h()/2}
	for _, to := range []point{
		{primary.Left - 3000, center.Y}, {primary.Right + 3000, center.Y},
		{center.X, primary.Top - 3000}, {center.X, primary.Bottom + 3000},
	} {
		drag(tabPoint(), to)
		check("dragged past an edge")
		p.endMove(true)
	}
	if len(moved) != 4 || moved[3] == nil {
		t.Fatalf("OnMove calls: %v", moved)
	}
	if r := windowRect(p.hwnd); moved[3].X != r.Left || moved[3].Y != r.Top {
		t.Errorf("saved %v, panel at %v", *moved[3], r)
	}

	// Onto every screen (a different scale lays it out again).
	for _, s := range screens() {
		t.Logf("screen %v", s)
		to := point{s.Left + s.w()/2, s.Top + s.h()/2}
		drag(tabPoint(), to)
		r := check("dragged to a screen")
		_, scale := monitorAt(to)
		if p.scale != scale || !inside(r, s) {
			t.Errorf("on screen %v: panel %v at scale %v, want %v", s, r, p.scale, scale)
		}
		p.endMove(true)
	}

	// Taller pages push it back onto the screen; shorter ones let it return.
	drag(tabPoint(), point{center.X, primary.Bottom + 3000})
	p.endMove(true)
	saved := *p.custom
	p.SetRows(tall)
	check("taller page at the bottom")
	p.SetRows(short)
	if r := check("short page again"); r.Top != saved.Y {
		t.Errorf("back on a short page at %d, saved %d", r.Top, saved.Y)
	}

	// Esc puts it back where it was.
	before := windowRect(p.hwnd)
	n := len(moved)
	drag(tabPoint(), center)
	p.escape()
	if r := check("Esc"); r != before || len(moved) != n || !p.Visible() {
		t.Errorf("after Esc: %v, want %v (OnMove calls %d, visible %v)", r, before, len(moved)-n, p.Visible())
	}

	// A position on no screen (unplugged, rearranged) docks it, but is kept.
	p.SetPosition(&PanelPos{-40000, -40000})
	check("lost screen")
	if !p.Moved() {
		t.Error("lost screen: the saved position was dropped")
	}
	if r, want := windowRect(p.hwnd), dockedPos(icon, primary, p.width, p.height, c.px(popupGapL), p.tab.visibleH(c)); r.Left != want.X || r.Top != want.Y {
		t.Errorf("lost screen: at %v, want docked %v", r, want)
	}

	// The mouse's back button: DefWindowProc turns it into WM_APPCOMMAND.
	procSendMessageW.Call(p.hwnd, wmXButtonUp, 1<<16 /* XBUTTON1 */, 0)
	if backs != 1 {
		t.Errorf("back button over the panel: %d backs", backs)
	}
	procSendMessageW.Call(p.tab.hwnd, wmXButtonUp, 1<<16, 0)
	if backs != 2 {
		t.Errorf("back button over the tab: %d backs", backs)
	}
	procSendMessageW.Call(p.hwnd, wmXButtonUp, 2<<16 /* XBUTTON2: forward */, 0)
	p.SetRows([]Row{Text("No header", "", ToneNormal)})
	procSendMessageW.Call(p.hwnd, wmXButtonUp, 1<<16, 0)
	if backs != 2 || !p.Visible() {
		t.Errorf("forward, or back without a back button: %d backs, visible %v", backs, p.Visible())
	}

	// Reset docks it and tells OnMove.
	p.SetPosition(&PanelPos{center.X, center.Y})
	p.ResetPosition()
	check("reset")
	if p.Moved() || moved[len(moved)-1] != nil {
		t.Errorf("reset: moved %v, last OnMove %v", p.Moved(), moved[len(moved)-1])
	}
}
