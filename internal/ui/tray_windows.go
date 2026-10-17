package ui

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	trayClass   = "ResoundTray"
	wmTray      = wmApp + 1 // tray icon callback
	wmInvoke    = wmApp + 2 // run queued functions on the UI thread
	wmShowPanel = wmApp + 3 // sent by a second instance
	wmQuitApp   = wmApp + 4 // sent by the installer/uninstaller
)

// Tray owns the notification-area icon, a hidden message window and the
// popup panel. Create it and call Run on the same, locked OS thread.
type Tray struct {
	hwnd           uintptr
	nid            notifyIconData
	popup          *Popup
	menu           *trayMenu // right-click: Show panel, Quit
	taskbarCreated uint32

	qmu   sync.Mutex
	queue []func()

	// OnQuitRequest runs on the UI thread when another process (installer,
	// uninstaller) asks this instance to exit.
	OnQuitRequest func()

	// OnQuit runs on the UI thread when the user chooses Quit in the tray
	// icon's right-click menu. Without it, the tray just quits.
	OnQuit func()

	// OnEndSession runs on the UI thread when Windows signs out, restarts
	// or shuts down. The process ends right after it returns, without Run
	// returning, so it must leave nothing behind.
	OnEndSession func()
}

// NewTray creates the message window, the popup and the tray icon. It locks
// the calling goroutine to its OS thread, which becomes the UI thread.
func NewTray(tooltip string) (*Tray, error) {
	runtime.LockOSThread()
	// The shell dialogs (folder picker) need COM in apartment mode on the UI
	// thread. Audio COM work runs on its own threads (see package wincom).
	_ = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	t := &Tray{}
	if err := registerClass(trayClass, 0, windows.NewCallback(t.wndProc)); err != nil {
		return nil, fmt.Errorf("registering tray class: %w", err)
	}
	h, err := createWindow(0, 0, trayClass)
	if err != nil {
		return nil, fmt.Errorf("creating tray window: %w", err)
	}
	t.hwnd = h
	if t.popup, err = newPopup(); err != nil {
		return nil, fmt.Errorf("creating popup: %w", err)
	}
	t.menu, err = newTrayMenu([2]Action{
		{Label: "Show panel", OnClick: t.showPanelFromMenu},
		{Label: "Quit", Icon: IconPower, Style: ButtonDanger, OnClick: t.quitFromMenu},
	})
	if err != nil {
		return nil, fmt.Errorf("creating tray menu: %w", err)
	}
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16Ptr("TaskbarCreated"))))
	t.taskbarCreated = uint32(r)

	t.nid = notifyIconData{
		Wnd:             h,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTray,
		Icon:            loadAppIcon(),
	}
	t.nid.Size = uint32(unsafe.Sizeof(t.nid))
	t.SetTooltip(tooltip)
	if err := t.notify(nimAdd); err != nil {
		return nil, fmt.Errorf("adding tray icon: %w", err)
	}
	return t, nil
}

// Popup returns the panel.
func (t *Tray) Popup() *Popup { return t.popup }

// Post runs fn on the UI thread. Safe from any goroutine.
func (t *Tray) Post(fn func()) {
	t.qmu.Lock()
	t.queue = append(t.queue, fn)
	t.qmu.Unlock()
	procPostMessageW.Call(t.hwnd, wmInvoke, 0, 0)
}

// SetTooltip changes the hover text of the icon (max 127 characters).
// Call it on the UI thread.
func (t *Tray) SetTooltip(s string) {
	u, _ := windows.UTF16FromString(s)
	n := copy(t.nid.Tip[:len(t.nid.Tip)-1], u)
	t.nid.Tip[n] = 0
	if t.nid.Tip[len(t.nid.Tip)-1] != 0 {
		t.nid.Tip[len(t.nid.Tip)-1] = 0
	}
	if t.hwnd != 0 {
		_ = t.notify(nimModify)
	}
}

// Run pumps messages until Quit is called.
func (t *Tray) Run() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// Quit removes the icon and ends Run. Call it on the UI thread.
func (t *Tray) Quit() {
	t.menu.hide()
	t.popup.Hide()
	_ = t.notify(nimDelete)
	procPostQuitMessage.Call(0)
}

// Hwnd returns the hidden message window, usable as a dialog owner.
func (t *Tray) Hwnd() uintptr { return t.hwnd }

func (t *Tray) notify(op uintptr) error {
	if r, _, err := procShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&t.nid))); r == 0 {
		return err
	}
	return nil
}

func (t *Tray) togglePopup() {
	switch {
	case t.popup.Visible():
		t.popup.Hide()
	case t.popup.recentlyHidden():
		// This click closed it through deactivation; leave it closed.
	default:
		t.popup.ShowAt(cursorPos())
	}
}

// toggleMenu opens the right-click menu at the cursor, or closes it.
func (t *Tray) toggleMenu() {
	switch {
	case t.menu.visible():
		t.menu.hide()
	case t.menu.recentlyHidden():
		// This click closed it by taking the focus away; leave it closed.
	default:
		t.menu.show(cursorPos())
	}
}

// showPanelFromMenu opens the panel, or brings it to the front when it is
// open already (e.g. behind a window that has the focus).
func (t *Tray) showPanelFromMenu() {
	if t.popup.Visible() {
		procSetForegroundWindow.Call(t.popup.hwnd)
		return
	}
	t.ShowPanel()
}

func (t *Tray) quitFromMenu() {
	if t.OnQuit != nil {
		t.OnQuit()
	} else {
		t.Quit()
	}
}

func (t *Tray) runQueue() {
	t.qmu.Lock()
	q := t.queue
	t.queue = nil
	t.qmu.Unlock()
	for _, fn := range q {
		fn()
	}
}

func (t *Tray) wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch {
	case m == wmTray:
		// Left-click opens or closes the panel; right-click opens the small
		// menu with Show panel and Quit.
		switch uint32(l) {
		case wmLButtonUp:
			t.menu.hide()
			t.togglePopup()
		case wmRButtonUp, wmContextMenu:
			t.toggleMenu()
		}
		return 0
	case m == wmInvoke:
		t.runQueue()
		return 0
	case m == wmShowPanel:
		t.ShowPanel()
		return 0
	case m == wmQuitApp:
		if t.OnQuitRequest != nil {
			t.OnQuitRequest()
		} else {
			t.Quit()
		}
		return 0
	case m == wmEndSession:
		if w != 0 && t.OnEndSession != nil {
			t.OnEndSession()
		}
		return 0
	case m == t.taskbarCreated && m != 0:
		_ = t.notify(nimAdd) // Explorer restarted
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

var procLoadIconMetric = windows.NewLazySystemDLL("comctl32.dll").NewProc("LoadIconMetric")

// loadAppIcon loads the "APP" icon group embedded by go-winres at the tray's
// size for the current DPI (LoadIconMetric picks the matching image from
// the .ico instead of shrinking the large one). It falls back to the stock
// application icon when the exe was built without resources.
func loadAppIcon() uintptr {
	if procLoadIconMetric.Find() == nil {
		var h uintptr
		const limSmall = 0
		if hr, _, _ := procLoadIconMetric.Call(moduleHandle(), uintptr(unsafe.Pointer(utf16Ptr("APP"))), limSmall, uintptr(unsafe.Pointer(&h))); hr == 0 && h != 0 {
			return h
		}
	}
	h, _, _ := procLoadImageW.Call(moduleHandle(), uintptr(unsafe.Pointer(utf16Ptr("APP"))), imageIcon, 0, 0, lrDefaultSize|lrShared)
	if h != 0 {
		return h
	}
	h, _, _ = procLoadIconW.Call(0, idiApplication)
	return h
}

// SignalRunningInstance asks an already running instance to open its panel.
// It returns false when no instance window was found.
func SignalRunningInstance() bool {
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(utf16Ptr(trayClass))), 0)
	if h == 0 {
		return false
	}
	procPostMessageW.Call(h, wmShowPanel, 0, 0)
	return true
}

var procShellNotifyIconGetRect = shell32.NewProc("Shell_NotifyIconGetRect")

// notifyIconIdentifier mirrors NOTIFYICONIDENTIFIER (40 bytes on x64).
type notifyIconIdentifier struct {
	Size uint32
	Wnd  uintptr
	ID   uint32
	GUID windows.GUID
}

// ShowPanel opens the panel next to the tray icon (or, when the icon sits
// in the hidden-icons overflow, next to the taskbar corner). For opening
// without a click on the icon, e.g. from the welcome notice or a second start.
func (t *Tray) ShowPanel() {
	if t.popup.Visible() {
		return
	}
	id := notifyIconIdentifier{Wnd: t.hwnd, ID: t.nid.ID}
	id.Size = uint32(unsafe.Sizeof(id))
	var r rect
	if hr, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&r))); hr == 0 && r.w() > 0 {
		t.popup.ShowAt(point{r.Left + r.w()/2, r.Top + r.h()/2})
		return
	}
	work, _ := taskbarWorkArea()
	t.popup.ShowAt(point{work.Right - 1, work.Bottom - 1})
}

// TogglePanel opens the panel like ShowPanel, or closes it if it is open,
// and reports whether it is open now. For the phone's long press on the
// logo. The panel is topmost, so it shows even when Windows keeps the focus
// with the app in front (it only hands the focus over after the user's own
// input).
func (t *Tray) TogglePanel() bool {
	if t.popup.Visible() {
		t.popup.Hide()
		return false
	}
	t.ShowPanel()
	return t.popup.Visible()
}

// SignalQuitRunningInstance asks a running instance to exit and waits up to
// timeout for it to close its window. It returns false if one is still
// running afterwards; true also when none was running.
func SignalQuitRunningInstance(timeout time.Duration) bool {
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(utf16Ptr(trayClass))), 0)
	if h == 0 {
		return true
	}
	procPostMessageW.Call(h, wmQuitApp, 0, 0)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(utf16Ptr(trayClass))), 0); h == 0 {
			time.Sleep(300 * time.Millisecond) // let the process finish exiting
			return true
		}
	}
	return false
}
