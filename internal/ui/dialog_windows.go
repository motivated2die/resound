package ui

// Styled confirmation dialog (ported from the Skira Seed Helper).
//
// It owns its window, message loop and OS thread, so it works from any
// goroutine and in processes without a tray (installer, uninstaller). If it
// cannot be shown, Confirm falls back to a native MessageBox.
//
// Safeguards:
//   - focus starts on Cancel unless DefaultConfirm is set, and Enter/Space
//     answer for the focused button, so a stray key press is harmless;
//   - input in the first moments after the window appears is ignored, so a
//     click or key meant for another window can't answer it;
//   - closing the window (Esc, Alt+F4) means Cancel.

import (
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DialogSpec describes a dialog.
type DialogSpec struct {
	Title, Message  string
	Confirm, Cancel string // button labels
	Danger          bool   // confirm button in the destructive colour
	DefaultConfirm  bool   // focus starts on the confirm button

	// Corner shows the dialog at the bottom-right of the taskbar's monitor,
	// next to the tray; Passive also never takes focus. Together they make
	// a non-intrusive notice.
	Corner, Passive bool

	// TrayHint is an extra paragraph with a small "^" box beside it: the
	// taskbar's hidden-icons arrow, so users know what to look for.
	TrayHint string
}

const (
	dialogClass      = "ResoundDialog"
	dialogInputGuard = 400 * time.Millisecond

	dlgWidthL    = 400
	dlgPadL      = 20
	dlgTitleHL   = 24
	dlgGapL      = 10
	dlgButtonHL  = 34
	dlgButtonMin = 96
	dlgHintBoxL  = 28
	dlgHintGapL  = 10
	dlgCornerL   = 16

	wsExNoActivate = 0x08000000
	vkSpace        = 0x20
	vkTab          = 0x09
	vkLeft         = 0x25
	vkRight        = 0x27

	btnCancel = 0
	btnOK     = 1
)

var (
	procGetWindowRect   = user32.NewProc("GetWindowRect")
	procPostMessageQuit = procPostQuitMessage

	dialogClassOnce sync.Once
	dialogClassErr  error
	dialogs         sync.Map // hwnd -> *dialog
)

type dialog struct {
	hwnd    uintptr
	spec    DialogSpec
	scale   float64
	fonts   *fonts
	width   int32
	height  int32
	msgH    int32
	hintH   int32
	focus   int
	hover   int
	shownAt time.Time
	result  bool
	track   bool
}

// Confirm shows the dialog and blocks until it is answered. It returns true
// only for the confirm button.
func Confirm(spec DialogSpec) bool {
	if ok, shown := TryConfirm(spec); shown {
		return ok
	}
	flags := uint32(windows.MB_YESNO | windows.MB_ICONQUESTION | windows.MB_SYSTEMMODAL)
	if !spec.DefaultConfirm {
		flags |= windows.MB_DEFBUTTON2
	}
	r, _ := windows.MessageBox(0, utf16Ptr(spec.Message), utf16Ptr(spec.Title), flags)
	return r == 6 // IDYES
}

// TryConfirm is Confirm without the fallback: shown is false when the
// dialog could not be displayed.
func TryConfirm(spec DialogSpec) (confirmed, shown bool) {
	answer := make(chan *bool, 1)
	go func() {
		runtime.LockOSThread() // the window and its messages stay on this thread
		answer <- runDialog(spec)
	}()
	if r := <-answer; r != nil {
		return *r, true
	}
	return false, false
}

func runDialog(spec DialogSpec) *bool {
	dialogClassOnce.Do(func() {
		dialogClassErr = registerClass(dialogClass, csDropShadow, windows.NewCallback(dialogWndProc))
	})
	if dialogClassErr != nil {
		return nil
	}
	ex := uint32(wsExTopmost | wsExToolWindow)
	if spec.Passive {
		ex |= wsExNoActivate
	}
	hwnd, err := createWindow(ex, wsPopup, dialogClass)
	if err != nil {
		return nil
	}
	d := &dialog{hwnd: hwnd, spec: spec, scale: 1, hover: -1}
	if spec.DefaultConfirm && !spec.Passive {
		d.focus = btnOK
	}
	dialogs.Store(hwnd, d)
	defer func() {
		dialogs.Delete(hwnd)
		if d.fonts != nil {
			d.fonts.destroy()
		}
	}()
	if !d.show() {
		procDestroyWindow.Call(hwnd)
		return nil
	}
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return &d.result
}

func (d *dialog) canvas(dc uintptr) *canvas { return &canvas{dc: dc, scale: d.scale, fonts: d.fonts} }

func (d *dialog) show() bool {
	var work rect
	if d.spec.Corner {
		work, d.scale = taskbarWorkArea()
	} else {
		work, d.scale = monitorAt(cursorPos())
	}
	d.fonts = newFonts(d.scale)

	dc, _, _ := procGetDC.Call(0)
	c := d.canvas(dc)
	d.width = c.px(dlgWidthL)
	inner := d.width - 2*c.px(dlgPadL)
	d.msgH = c.wrapHeight(d.spec.Message, inner, fontBody)
	d.height = 2*c.px(dlgPadL) + c.px(dlgTitleHL) + c.px(dlgGapL) + d.msgH + c.px(dlgPadL) + c.px(dlgButtonHL)
	if d.spec.TrayHint != "" {
		d.hintH = max(c.px(dlgHintBoxL), c.wrapHeight(d.spec.TrayHint, inner-c.px(dlgHintBoxL+dlgHintGapL), fontBody))
		d.height += c.px(dlgGapL) + d.hintH
	}
	procReleaseDC.Call(0, dc)

	x := work.Left + (work.w()-d.width)/2
	y := work.Top + (work.h()-d.height)/2
	if d.spec.Corner {
		x = work.Right - d.width - c.px(dlgCornerL)
		y = work.Bottom - d.height - c.px(dlgCornerL)
	}
	flags := uintptr(swpShowWindow)
	if d.spec.Passive {
		flags |= swpNoActivate
	}
	if procDwmSetWindowAttribute.Find() == nil {
		pref := uint32(dwmwcpRound)
		procDwmSetWindowAttribute.Call(d.hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), 4)
	}
	d.shownAt = time.Now()
	if r, _, _ := procSetWindowPos.Call(d.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(d.width), uintptr(d.height), flags); r == 0 {
		return false
	}
	if !d.spec.Passive {
		procSetForegroundWindow.Call(d.hwnd)
	}
	procInvalidateRect.Call(d.hwnd, 0, 0)
	r, _, _ := procIsWindowVisible.Call(d.hwnd)
	return r != 0
}

// taskbarWorkArea returns the work area and scale of the taskbar's monitor.
func taskbarWorkArea() (rect, float64) {
	if tb, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(utf16Ptr("Shell_TrayWnd"))), 0); tb != 0 {
		var r rect
		if procGetWindowRect.Find() == nil {
			procGetWindowRect.Call(tb, uintptr(unsafe.Pointer(&r)))
			return monitorAt(point{r.Left + r.w()/2, r.Top + r.h()/2})
		}
	}
	return monitorAt(point{0, 0})
}

func (d *dialog) buttonRect(btn int) rect {
	c := d.canvas(0)
	dc, _, _ := procGetDC.Call(0)
	c.dc = dc
	labelW := func(s string) int32 { return max(c.px(dlgButtonMin), c.textWidth(s, fontBold)+c.px(32)) }
	okW, cancelW := labelW(d.spec.Confirm), labelW(d.spec.Cancel)
	procReleaseDC.Call(0, dc)
	y1 := d.height - c.px(dlgPadL)
	y0 := y1 - c.px(dlgButtonHL)
	okR := d.width - c.px(dlgPadL)
	if btn == btnOK {
		return rect{okR - okW, y0, okR, y1}
	}
	cancelR := okR - okW - c.px(10)
	return rect{cancelR - cancelW, y0, cancelR, y1}
}

func (d *dialog) hit(x, y int32) int {
	for _, b := range []int{btnCancel, btnOK} {
		r := d.buttonRect(b)
		if x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom {
			return b
		}
	}
	return -1
}

func (d *dialog) finish(ok bool) {
	d.result = ok
	procDestroyWindow.Call(d.hwnd)
}

func (d *dialog) acceptsInput() bool { return time.Since(d.shownAt) >= dialogInputGuard }

func dialogWndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	v, ok := dialogs.Load(hwnd)
	if !ok {
		return defWindowProc(hwnd, m, w, l)
	}
	d := v.(*dialog)
	switch m {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		d.paint()
		return 0
	case wmClose:
		d.finish(false)
		return 0
	case wmDestroy:
		procPostMessageQuit.Call(0)
		return 0
	case wmMouseMove:
		if !d.track {
			tme := trackMouseEventStruct{Flags: tmeLeave, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			d.track = true
		}
		if h := d.hit(loword(l), hiword(l)); h != d.hover {
			d.hover = h
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmMouseLeave:
		d.track = false
		d.hover = -1
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonUp:
		if b := d.hit(loword(l), hiword(l)); b >= 0 && d.acceptsInput() {
			d.finish(b == btnOK)
		}
		return 0
	case wmKeyDown:
		switch w {
		case vkEscape:
			d.finish(false)
		case vkReturn, vkSpace:
			if d.acceptsInput() {
				d.finish(d.focus == btnOK)
			}
		case vkTab, vkLeft, vkRight:
			d.focus = 1 - d.focus
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

func (d *dialog) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(d.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(d.hwnd, uintptr(unsafe.Pointer(&ps)))
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(d.width), uintptr(d.height))
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer func() {
		procSelectObject.Call(mem, old)
		procDeleteObject.Call(bmp)
		procDeleteDC.Call(mem)
	}()
	c := d.canvas(mem)
	c.frame(rect{0, 0, d.width, d.height}, 0, colBackground, colDivider)

	pad := c.px(dlgPadL)
	x0, x1 := pad, d.width-pad
	c.text(d.spec.Title, rect{x0, pad, x1, pad + c.px(dlgTitleHL)}, fontTitle, colText, dtLeft)
	msgY := pad + c.px(dlgTitleHL) + c.px(dlgGapL)
	c.textWrapped(d.spec.Message, rect{x0, msgY, x1, msgY + d.msgH}, fontBody, colMuted)

	if d.spec.TrayHint != "" {
		hintY := msgY + d.msgH + c.px(dlgGapL)
		box := c.px(dlgHintBoxL)
		boxY := hintY + (d.hintH-box)/2
		c.frame(rect{x0, boxY, x0 + box, boxY + box}, c.px(6), colControl, colMuted)
		c.chevron(x0+box/2, boxY+box/2, c.px(5), true, colText)
		textX := x0 + box + c.px(dlgHintGapL)
		th := c.wrapHeight(d.spec.TrayHint, x1-textX, fontBody)
		c.textWrapped(d.spec.TrayHint, rect{textX, hintY + (d.hintH-th)/2, x1, hintY + d.hintH}, fontBody, colMuted)
	}

	for _, b := range []int{btnCancel, btnOK} {
		r := d.buttonRect(b)
		label, fill, fg, border := d.spec.Cancel, colBackground, colText, colDivider
		if b == btnOK {
			label, fill, fg, border = d.spec.Confirm, colAccent, colAccentText, colAccent
			if d.spec.Danger {
				fill, fg, border = colDanger, colText, colDanger
			}
		}
		if d.hover == b {
			fill = shade(fill, 14)
		}
		if d.focus == b && !d.spec.Passive {
			border = colText // shows what Enter/Space will answer
		}
		c.frame(r, c.px(8), fill, border)
		c.text(label, r, fontBold, fg, dtCenter)
	}
	procBitBlt.Call(hdc, 0, 0, uintptr(d.width), uintptr(d.height), mem, 0, 0, 0x00CC0020)
}
