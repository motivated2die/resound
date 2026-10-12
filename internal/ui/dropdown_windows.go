package ui

// Dropdown: a field that opens a scrollable option list. The list is drawn
// as an overlay inside the panel window (not a separate window), so opening
// it never moves the focus away from the panel — which would close it.
//
// Behaviour:
//   - opens below the field, or above when there is more room there;
//   - shows up to dropdownMaxItems options and scrolls (wheel, arrow keys);
//   - Up/Down/Home/End move, Enter selects, Esc or a click elsewhere closes;
//   - hovering a truncated option (or the field) shows its full text in a
//     wrapping tooltip after a short delay.

// Option is one entry of a dropdown.
type Option struct {
	Label  string
	Detail string // short tag shown on the right, e.g. "default"
}

func (o Option) full() string {
	if o.Detail == "" {
		return o.Label
	}
	return o.Label + " (" + o.Detail + ")"
}

const (
	dropdownMaxItems = 8
	dropdownItemL    = 32 // logical item height
	tipDelay         = 450
	tipTimerID       = 7
)

type dropdownRow struct {
	id          string
	label       string
	options     []Option
	selected    int
	placeholder string
	onSelect    func(int)
}

// Dropdown is a labelled select field. id must be unique on the page and
// stable across re-renders (it keeps an open list attached to its row).
// selected is an index into options, or -1 to show the placeholder.
// An empty label gives a compact field without caption.
func Dropdown(id, label, placeholder string, options []Option, selected int, onSelect func(int)) Row {
	if placeholder == "" {
		placeholder = "Choose…"
	}
	return dropdownRow{id: id, label: label, options: options, selected: selected, placeholder: placeholder, onSelect: onSelect}
}

func (d dropdownRow) height() int {
	if d.label == "" {
		return 44
	}
	return 62
}

func (d dropdownRow) segments(c *canvas, r rect) []rect {
	in := inset(c, r)
	top := r.Top + c.px(5)
	if d.label != "" {
		top = r.Top + c.px(22)
	}
	return []rect{{in.Left, top, in.Right, top + c.px(34)}}
}

func (dropdownRow) click(int)       {} // handled by the popup (see open)
func (dropdownRow) keepsOpen() bool { return true }

func (d dropdownRow) current() (Option, bool) {
	if d.selected >= 0 && d.selected < len(d.options) {
		return d.options[d.selected], true
	}
	return Option{}, false
}

func (d dropdownRow) paint(c *canvas, r rect, st rowState) {
	in := inset(c, r)
	if d.label != "" {
		c.text(upper(d.label), rect{in.Left, r.Top + c.px(4), in.Right, r.Top + c.px(20)}, fontCaption, colCaption, dtLeft)
	}
	field := d.segments(c, r)[0]
	bg, border := colControl, colControl
	switch {
	case st.open:
		border = colAccent
	case st.press == 0:
		bg = shade(bg, -12)
	case st.hover == 0:
		bg = shade(bg, 12)
	}
	c.frame(field, c.px(10), bg, border)

	pad := c.px(12)
	chevX := field.Right - pad - c.px(5)
	c.chevron(chevX, field.Top+field.h()/2, c.px(5), st.open, colMuted)
	textRight := chevX - c.px(14)

	opt, ok := d.current()
	if !ok {
		c.text(d.placeholder, rect{field.Left + pad, field.Top, textRight, field.Bottom}, fontBody, colMuted, dtLeft)
		return
	}
	if opt.Detail != "" {
		w := c.textWidth(opt.Detail, fontCaption) + c.px(4)
		c.text(opt.Detail, rect{textRight - w, field.Top, textRight, field.Bottom}, fontCaption, colAccent, dtRight)
		textRight -= w + c.px(8)
	}
	c.text(opt.Label, rect{field.Left + pad, field.Top, textRight, field.Bottom}, fontBody, colText, dtLeft)
}

// tip returns the full selection when it does not fit the field.
func (d dropdownRow) tip(c *canvas, r rect, _ int) string {
	opt, ok := d.current()
	if !ok {
		return ""
	}
	field := d.segments(c, r)[0]
	avail := field.w() - 2*c.px(12) - c.px(24)
	if opt.Detail != "" {
		avail -= c.textWidth(opt.Detail, fontCaption) + c.px(12)
	}
	if c.textWidth(opt.Label, fontBody) <= avail {
		return ""
	}
	return opt.full()
}

func (d dropdownRow) open(p *Popup, row int) {
	p.dd = &dropdownState{id: d.id, row: row, hover: d.selected, press: -1}
	p.syncDropdown()
	if p.dd != nil {
		p.dd.scrollTo(d.selected)
	}
	p.hideTip()
	procInvalidateRect.Call(p.hwnd, 0, 0)
}

// --- popup side ------------------------------------------------------------

// opener is implemented by rows that open something inside the popup.
type opener interface{ open(p *Popup, row int) }

// tipper is implemented by rows that can show a full-text tooltip.
type tipper interface {
	tip(c *canvas, r rect, seg int) string
}

type dropdownState struct {
	id       string
	row      int
	options  []Option
	selected int
	onSelect func(int)

	field  rect // window coordinates
	list   rect
	itemH  int32
	scroll int32 // in pixels
	hover  int
	press  int
}

type tooltip struct {
	key     string // what the tip belongs to; a change restarts the delay
	text    string
	anchor  rect
	visible bool
}

func (d *dropdownState) contentH() int32 { return int32(len(d.options)) * d.itemH }

func (d *dropdownState) maxScroll() int32 { return max(0, d.contentH()-d.list.h()) }

func (d *dropdownState) scrollTo(i int) {
	if i < 0 {
		return
	}
	top := int32(i) * d.itemH
	if top < d.scroll {
		d.scroll = top
	} else if top+d.itemH > d.scroll+d.list.h() {
		d.scroll = top + d.itemH - d.list.h()
	}
	d.scroll = max(0, min(d.scroll, d.maxScroll()))
}

func (d *dropdownState) itemRect(i int) rect {
	top := d.list.Top + int32(i)*d.itemH - d.scroll
	return rect{d.list.Left, top, d.list.Right, top + d.itemH}
}

// itemAt returns the option under (x, y), -1 for none, or -2 outside the list.
func (d *dropdownState) itemAt(x, y int32) int {
	if x < d.list.Left || x >= d.list.Right || y < d.list.Top || y >= d.list.Bottom {
		return -2
	}
	i := int((y - d.list.Top + d.scroll) / d.itemH)
	if i >= len(d.options) {
		return -1
	}
	return i
}

// syncDropdown re-attaches an open list to its row after a re-layout (rows
// are rebuilt on every state change) and recomputes where it opens.
func (p *Popup) syncDropdown() {
	d := p.dd
	if d == nil {
		return
	}
	row := -1
	for i, s := range p.slots {
		if dr, ok := s.row.(dropdownRow); ok && dr.id == d.id {
			row = i
			d.options, d.selected, d.onSelect = dr.options, dr.selected, dr.onSelect
			break
		}
	}
	if row < 0 || len(d.options) == 0 {
		p.dd = nil
		return
	}
	c := p.canvas(0)
	d.row = row
	d.field = p.slots[row].row.segments(c, p.rowRect(row))[0]
	d.itemH = c.px(dropdownItemL)
	margin := c.px(6)
	want := int32(min(len(d.options), dropdownMaxItems))*d.itemH + 2*c.px(4)
	below := p.height - d.field.Bottom - margin - c.px(4)
	above := d.field.Top - margin - c.px(4)
	if want <= below || below >= above {
		h := min(want, max(below, d.itemH))
		d.list = rect{d.field.Left, d.field.Bottom + c.px(4), d.field.Right, d.field.Bottom + c.px(4) + h}
	} else {
		h := min(want, max(above, d.itemH))
		d.list = rect{d.field.Left, d.field.Top - c.px(4) - h, d.field.Right, d.field.Top - c.px(4)}
	}
	// Inner padding: the list rect used for items excludes a 4px border area.
	d.list = rect{d.list.Left, d.list.Top + c.px(4), d.list.Right, d.list.Bottom - c.px(4)}
	d.scroll = max(0, min(d.scroll, d.maxScroll()))
	if d.hover >= len(d.options) {
		d.hover = -1
	}
}

func (p *Popup) closeDropdown() {
	if p.dd != nil {
		p.dd = nil
		p.hideTip()
		procInvalidateRect.Call(p.hwnd, 0, 0)
	}
}

func (p *Popup) chooseDropdown(i int) {
	d := p.dd
	p.closeDropdown()
	if d != nil && i >= 0 && i < len(d.options) && i != d.selected && d.onSelect != nil {
		d.onSelect(i)
	}
}

// dropdownMessage handles input while a list is open. It returns true when
// the message was consumed.
func (p *Popup) dropdownMessage(m uint32, w, l uintptr) bool {
	d := p.dd
	if d == nil {
		return false
	}
	x, y := loword(l), hiword(l)
	switch m {
	case wmMouseMove:
		i := d.itemAt(x, y)
		if i < 0 {
			i = -1
		}
		if i != d.hover {
			d.hover = i
			procInvalidateRect.Call(p.hwnd, 0, 0)
		}
		p.setHover(-1, -1)
		p.armTip(dropdownTipKey(d, i))
		return false // let tracking (WM_MOUSELEAVE) be set up as usual
	case wmLButtonDown:
		d.press = d.itemAt(x, y)
		if d.press == -2 { // outside: close; a click on the field just closes
			p.closeDropdown()
		}
		return true
	case wmLButtonUp:
		if i := d.itemAt(x, y); i >= 0 && i == d.press {
			p.chooseDropdown(i)
		}
		if p.dd != nil {
			p.dd.press = -1
		}
		return true
	case wmMouseWheel:
		d.scroll = max(0, min(d.scroll-hiword(w)*d.itemH/120, d.maxScroll()))
		p.hideTip()
		procInvalidateRect.Call(p.hwnd, 0, 0)
		return true
	case wmKeyDown:
		switch w {
		case vkEscape:
			p.closeDropdown()
		case vkReturn:
			p.chooseDropdown(d.hover)
		case vkUp, vkDown, vkHome, vkEnd:
			n := len(d.options)
			switch w {
			case vkUp:
				d.hover = max(0, d.hover-1)
			case vkDown:
				d.hover = min(n-1, d.hover+1)
			case vkHome:
				d.hover = 0
			case vkEnd:
				d.hover = n - 1
			}
			d.scrollTo(d.hover)
			p.armTip(dropdownTipKey(d, d.hover))
			procInvalidateRect.Call(p.hwnd, 0, 0)
		}
		return true
	}
	return false
}

func dropdownTipKey(d *dropdownState, i int) string {
	if i < 0 {
		return ""
	}
	return "dd:" + d.id + ":" + d.options[i].full()
}

func (p *Popup) paintDropdown(c *canvas) {
	d := p.dd
	if d == nil {
		return
	}
	pad4 := c.px(4)
	outer := rect{d.list.Left, d.list.Top - pad4, d.list.Right, d.list.Bottom + pad4}
	c.frame(outer, c.px(10), colList, colDivider)

	// Clip item drawing to the list area by skipping rows outside it and
	// trimming partially visible ones.
	for i, o := range d.options {
		r := d.itemRect(i)
		if r.Bottom <= d.list.Top || r.Top >= d.list.Bottom {
			continue
		}
		r.Top, r.Bottom = max(r.Top, d.list.Top), min(r.Bottom, d.list.Bottom)
		inner := rect{r.Left + pad4, r.Top, r.Right - pad4, r.Bottom}
		if i == d.hover {
			c.roundRect(inner, c.px(7), colRowHover)
		}
		textL := inner.Left + c.px(10)
		if i == d.selected {
			bar := rect{inner.Left + c.px(2), inner.Top + inner.h()/4, inner.Left + c.px(5), inner.Bottom - inner.h()/4}
			c.roundRect(bar, c.px(2), colAccent)
		}
		textR := inner.Right - c.px(10)
		if o.Detail != "" {
			w := c.textWidth(o.Detail, fontCaption) + c.px(4)
			c.text(o.Detail, rect{textR - w, inner.Top, textR, inner.Bottom}, fontCaption, colAccent, dtRight)
			textR -= w + c.px(8)
		}
		font := fontBody
		if i == d.selected {
			font = fontBold
		}
		c.text(o.Label, rect{textL, inner.Top, textR, inner.Bottom}, font, colText, dtLeft)
	}
	if ms := d.maxScroll(); ms > 0 {
		track := d.list.h()
		thumb := max(c.px(18), track*d.list.h()/d.contentH())
		top := d.list.Top + (track-thumb)*d.scroll/ms
		c.roundRect(rect{d.list.Right - c.px(5), top, d.list.Right - c.px(2), top + thumb}, c.px(3), colControl)
	}
}

// --- tooltips --------------------------------------------------------------

// armTip restarts the tooltip delay when the hovered target changes.
func (p *Popup) armTip(key string) {
	if key == p.tip.key {
		return
	}
	p.hideTip()
	p.tip.key = key
	if key != "" {
		procSetTimer.Call(p.hwnd, tipTimerID, tipDelay, 0)
	}
}

func (p *Popup) hideTip() {
	procKillTimer.Call(p.hwnd, tipTimerID)
	if p.tip.visible {
		procInvalidateRect.Call(p.hwnd, 0, 0)
	}
	p.tip = tooltip{}
}

// showTip runs when the delay elapsed: it measures whether the hovered
// text is cut off and, if so, shows it in full.
func (p *Popup) showTip() {
	procKillTimer.Call(p.hwnd, tipTimerID)
	dc, _, _ := procGetDC.Call(p.hwnd)
	if dc == 0 {
		return
	}
	defer procReleaseDC.Call(p.hwnd, dc)
	c := p.canvas(dc)

	text, anchor := "", rect{}
	if d := p.dd; d != nil {
		if d.hover >= 0 && d.hover < len(d.options) {
			o := d.options[d.hover]
			r := d.itemRect(d.hover)
			avail := r.w() - c.px(28)
			if o.Detail != "" {
				avail -= c.textWidth(o.Detail, fontCaption) + c.px(12)
			}
			if c.textWidth(o.Label, fontBody) > avail {
				text, anchor = o.full(), r
			}
		}
	} else if p.hoverRow >= 0 && p.hoverRow < len(p.slots) {
		if t, ok := p.slots[p.hoverRow].row.(tipper); ok {
			r := p.rowRect(p.hoverRow)
			text, anchor = t.tip(c, r, p.hoverSeg), r
		}
	}
	if text == "" {
		return
	}
	p.tip.text, p.tip.anchor, p.tip.visible = text, anchor, true
	procInvalidateRect.Call(p.hwnd, 0, 0)
}

func (p *Popup) paintTip(c *canvas) {
	if !p.tip.visible {
		return
	}
	pad := c.px(10)
	w := p.width - 2*c.px(10)
	textW := w - 2*pad
	h := c.wrapHeight(p.tip.text, textW, fontBody) + 2*pad - c.px(4)
	gap := c.px(4)
	top := p.tip.anchor.Bottom + gap
	if top+h > p.height-c.px(4) {
		top = p.tip.anchor.Top - gap - h
	}
	top = max(c.px(4), min(top, p.height-h-c.px(4)))
	box := rect{c.px(10), top, c.px(10) + w, top + h}
	c.frame(box, c.px(8), colTip, colDivider)
	c.textWrapped(p.tip.text, rect{box.Left + pad, box.Top + pad - c.px(2), box.Right - pad, box.Bottom}, fontBody, colText)
}
