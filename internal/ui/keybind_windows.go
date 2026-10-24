package ui

// Keybind: one function's keys in one scope (everywhere, or one app), as a
// card. Its header has the function's icon and name, an info icon whose
// tooltip explains it, and tools on the right: add a key, put the global
// keys back (an app's own keys), or cancel recording. Below, the keys are
// chips, each with a remove button. A card without keys is greyed out; an
// app's card that uses the global keys shows them muted, without remove
// buttons. While recording, a chip shows what was pressed so far, with a
// short hint and a list of ready-made keys below it.

// KeybindSpec describes a Keybind card.
type KeybindSpec struct {
	ID    string // unique on the page and stable across renders (its list)
	Icon  Icon   // the function's icon
	Title string // the function, e.g. "Push-to-talk"
	Info  string // the info icon's tooltip; "" for none
	Scope string // a caption at the right of the header, e.g. "Global"; "" for none

	Keys      []string // the combinations' names
	Inherited bool     // Keys are the global ones, shown in an app's card: muted, not removable
	Off       bool     // the function is off in this app; Keys are empty
	OffLabel  string   // the chip for Off, e.g. "None: off in this app"

	Recording bool     // waiting for a key or mouse button
	Pressed   string   // what was pressed so far while recording, "" for nothing yet
	Hint      string   // under the chip while recording
	Presets   []Option // offered in a list while recording
	OnPreset  func(int)

	Note     string // a remark under the keys, e.g. why a key wasn't added
	NoteTone Tone

	OnAdd    func() // the add tool; nil hides it (e.g. the list is full)
	AddTip   string
	OnCancel func()      // the cancel tool while recording
	OnRemove func(i int) // a key's remove button; nil: none
	OnReset  func()      // back to the global keys (an app's own keys or Off); nil hides it
	ResetTip string
	OnOff    func() // turns the function off in this app, left of the add tool; nil hides it
	OffTip   string

	Tool    Icon   // a further tool left of the others, e.g. push-to-talk's "send to game"; "" for none
	ToolTip string // its tooltip
	OnTool  func() // nil shows it disabled
}

type keybindRow struct{ s KeybindSpec }

// Keybind is a function's keys as a card; see KeybindSpec.
func Keybind(s KeybindSpec) Row { return keybindRow{s} }

// Segments: these first, then each chip's remove button.
const (
	kbSegInfo = iota
	kbSegAdd
	kbSegReset
	kbSegCancel
	kbSegField
	kbSegTool
	kbSegOff
	kbSegChips
)

// Logical sizes.
const (
	kbMarginL  = 3  // above and below the card, inside the row
	kbPadL     = 12 // inside the card
	kbHeaderL  = 36 // icon, title and tools
	kbToolL    = 26 // a tool button
	kbChipL    = 24 // a chip's height
	kbChipPadL = 9  // inside a chip, left and right of its text
	kbChipXL   = 16 // a chip's remove button
	kbGapL     = 6  // between chips, and between the parts below them
	kbHintL    = 18
	kbFieldL   = 30
	kbBottomL  = 10
)

type kbChip struct {
	text     string
	style    int // kbChipKey…
	removes  int // the key it removes (OnRemove), -1 for Off (OnReset), -2 for none
	box, del rect
}

const (
	kbChipKey = iota
	kbChipInherited
	kbChipOff
	kbChipRecording
)

// kbLayout is where everything of a card goes, in device pixels.
type kbLayout struct {
	box, icon, title, info, scope rect
	add, off, reset, cancel, tool rect
	chips                         []kbChip
	hint, field, note             rect
	height                        int32 // the row's
}

func (k keybindRow) chips() []kbChip {
	s := k.s
	switch {
	case s.Recording:
		text := s.Pressed + " …"
		if s.Pressed == "" {
			text = "Press a key…"
		}
		return []kbChip{{text: text, style: kbChipRecording, removes: -2}}
	case s.Off:
		removes := -2
		if s.OnReset != nil {
			removes = -1
		}
		return []kbChip{{text: s.OffLabel, style: kbChipOff, removes: removes}}
	case len(s.Keys) == 0:
		return nil // the greyed-out card says it: it collapses to its header
	}
	chips := make([]kbChip, len(s.Keys))
	for i, name := range s.Keys {
		chips[i] = kbChip{text: name, style: kbChipKey, removes: i}
		if s.Inherited {
			chips[i].style, chips[i].removes = kbChipInherited, -2
		} else if s.OnRemove == nil {
			chips[i].removes = -2
		}
	}
	return chips
}

func (k keybindRow) layout(c *canvas, r rect) kbLayout {
	s := k.s
	var l kbLayout
	in := inset(c, r)
	pad := c.px(kbPadL)
	l.box = rect{in.Left, r.Top + c.px(kbMarginL), in.Right, 0}
	top := l.box.Top

	// Header: icon, title, info icon on the left; tools on the right.
	tool := c.px(kbToolL)
	toolTop := top + (c.px(kbHeaderL)-tool)/2
	right := l.box.Right - c.px(5)
	place := func(show bool) rect {
		if !show {
			return rect{}
		}
		b := rect{right - tool, toolTop, right, toolTop + tool}
		right = b.Left - c.px(2)
		return b
	}
	if s.Recording {
		l.cancel = place(s.OnCancel != nil)
	} else {
		l.add = place(s.OnAdd != nil)
		l.off = place(s.OnOff != nil && !s.Off)
		l.reset = place(s.OnReset != nil && !s.Off)
		l.tool = place(s.Tool != "")
	}
	iconL := c.px(16)
	l.icon = rect{l.box.Left + pad, top, l.box.Left + pad + iconL, top + c.px(kbHeaderL)}
	titleLeft := l.icon.Right + c.px(8)
	if s.Scope != "" && !s.Recording {
		w := c.textWidth(upper(s.Scope), fontCaption)
		l.scope = rect{right - w - c.px(4), top, right - c.px(4), top + c.px(kbHeaderL)}
		right = l.scope.Left - c.px(6)
	}
	infoW := int32(0)
	if s.Info != "" {
		infoW = c.px(22)
	}
	titleW := min(c.textWidth(s.Title, fontBold)+c.px(2), right-titleLeft-infoW)
	l.title = rect{titleLeft, top, titleLeft + titleW, top + c.px(kbHeaderL) - c.px(1)}
	if s.Info != "" {
		size := c.px(20)
		y := top + (c.px(kbHeaderL)-size)/2
		l.info = rect{l.title.Right + c.px(2), y, l.title.Right + c.px(2) + size, y + size}
	}

	// Chips, wrapping.
	areaL, areaR := l.box.Left+pad, l.box.Right-pad
	x, y := areaL, top+c.px(kbHeaderL)
	chipH, gap := c.px(kbChipL), c.px(kbGapL)
	l.chips = k.chips()
	for i := range l.chips {
		ch := &l.chips[i]
		w := c.textWidth(ch.text, fontBody) + 2*c.px(kbChipPadL)
		if ch.removes != -2 {
			w += c.px(kbChipXL) - c.px(kbChipPadL)/2
		}
		w = min(w, areaR-areaL)
		if x > areaL && x+w > areaR {
			x, y = areaL, y+chipH+gap
		}
		ch.box = rect{x, y, x + w, y + chipH}
		if ch.removes != -2 {
			ch.del = rect{ch.box.Right - c.px(kbChipXL) - c.px(3), y, ch.box.Right, y + chipH}
		}
		x += w + gap
	}
	collapsed := len(l.chips) == 0
	if !collapsed {
		y += chipH
	}

	if s.Recording {
		if s.Hint != "" {
			h := max(c.px(kbHintL), c.wrapHeight(s.Hint, areaR-areaL, fontCaption))
			l.hint = rect{areaL, y + c.px(5), areaR, y + c.px(5) + h}
			y = l.hint.Bottom
		}
		if len(s.Presets) > 0 {
			l.field = rect{areaL, y + gap, areaR, y + gap + c.px(kbFieldL)}
			y = l.field.Bottom
		}
	}
	if s.Note != "" {
		h := c.wrapHeight(s.Note, areaR-areaL, fontBody)
		if collapsed {
			y -= gap // right under the header
		}
		l.note = rect{areaL, y + gap, areaR, y + gap + h}
		y = l.note.Bottom
		collapsed = false
	}
	l.box.Bottom = y + c.px(kbBottomL)
	if collapsed {
		l.box.Bottom = y // the header alone, its contents centred
	}
	l.height = l.box.Bottom + c.px(kbMarginL) - r.Top
	return l
}

func (keybindRow) height() int { return kbMarginL*2 + kbHeaderL }

func (k keybindRow) measure(c *canvas, width int32) int32 {
	return k.layout(c, rect{0, 0, width, 0}).height
}

func (k keybindRow) segments(c *canvas, r rect) []rect {
	l := k.layout(c, r)
	tool := l.tool
	if k.s.OnTool == nil {
		tool = rect{}
	}
	segs := []rect{l.info, l.add, l.reset, l.cancel, l.field, tool, l.off}
	for _, ch := range l.chips {
		segs = append(segs, ch.del)
	}
	return segs
}

func (k keybindRow) click(seg int) {
	s := k.s
	switch {
	case seg == kbSegAdd && s.OnAdd != nil:
		s.OnAdd()
	case seg == kbSegReset && s.OnReset != nil:
		s.OnReset()
	case seg == kbSegCancel && s.OnCancel != nil:
		s.OnCancel()
	case seg == kbSegTool && s.OnTool != nil:
		s.OnTool()
	case seg == kbSegOff && s.OnOff != nil:
		s.OnOff()
	case seg >= kbSegChips:
		chips := k.chips()
		if i := seg - kbSegChips; i < len(chips) {
			switch ch := chips[i]; {
			case ch.removes >= 0 && s.OnRemove != nil:
				s.OnRemove(ch.removes)
			case ch.removes == -1 && s.OnReset != nil:
				s.OnReset()
			}
		}
	}
}

func (keybindRow) keepsOpen() bool { return true }

// field is the list of ready-made keys while recording.
func (k keybindRow) field(seg int) (dropdownField, bool) {
	s := k.s
	if seg != kbSegField || !s.Recording || len(s.Presets) == 0 {
		return dropdownField{}, false
	}
	return dropdownField{id: s.ID + "-pick", options: s.Presets, selected: -1,
		placeholder: "Or choose one of these…", onSelect: s.OnPreset}, true
}

func (k keybindRow) tip(c *canvas, r rect, seg int) string {
	s := k.s
	switch seg {
	case kbSegInfo:
		return s.Info
	case kbSegAdd:
		return s.AddTip
	case kbSegReset:
		return s.ResetTip
	case kbSegCancel:
		return "Cancel (Esc)"
	case kbSegTool:
		return s.ToolTip
	case kbSegOff:
		return s.OffTip
	}
	if i := seg - kbSegChips; i >= 0 {
		if chips := k.chips(); i < len(chips) {
			if chips[i].removes == -1 {
				return "Use the global keys again"
			}
			return "Remove " + chips[i].text
		}
	}
	return ""
}

func (k keybindRow) paint(c *canvas, r rect, st rowState) {
	s := k.s
	l := k.layout(c, r)
	radius := c.px(12)
	greyed := !s.Recording && (s.Off || len(s.Keys) == 0)
	bg := colRowHover
	if greyed {
		bg = colBackground
		c.frame(l.box, radius, bg, colDivider)
	} else {
		c.roundRect(l.box, radius, bg)
	}

	fg, iconCol := colText, colText
	if greyed {
		fg, iconCol = colMuted, colCaption
	}
	if s.Recording {
		iconCol = colAccent
	}
	c.icon(s.Icon, l.icon, 16, iconCol)
	c.text(s.Title, l.title, fontBold, fg, dtLeft)
	if s.Info != "" {
		col := colCaption
		if st.hover == kbSegInfo {
			col = colText
		}
		c.icon(IconInfo, l.info, 14, col)
	}
	if l.scope != (rect{}) {
		c.text(upper(s.Scope), l.scope, fontCaption, colCaption, dtRight)
	}
	paintTool := func(seg int, box rect, paint func(col uint32)) {
		if box == (rect{}) {
			return
		}
		switch {
		case st.press == seg:
			c.roundRect(box, c.px(8), shade(bg, 22))
		case st.hover == seg:
			c.roundRect(box, c.px(8), shade(bg, 12))
		}
		paint(colText)
	}
	paintTool(kbSegAdd, l.add, func(col uint32) { c.icon(IconPlus, l.add, 18, col) })
	paintTool(kbSegOff, l.off, func(col uint32) { c.icon(IconBan, l.off, 16, col) })
	paintTool(kbSegReset, l.reset, func(col uint32) { c.icon(IconRestore, l.reset, 16, col) })
	paintTool(kbSegCancel, l.cancel, func(col uint32) { c.icon(IconClose, l.cancel, 16, col) })
	if s.Tool != "" {
		toolSeg := kbSegTool
		if s.OnTool == nil {
			toolSeg = -2 // never hovered
		}
		paintTool(toolSeg, l.tool, func(col uint32) {
			if s.OnTool == nil {
				col = colCaption
			}
			c.icon(s.Tool, l.tool, 16, col)
		})
	}

	for i, ch := range l.chips {
		seg := kbSegChips + i
		text := ch.box
		text.Left += c.px(kbChipPadL)
		text.Right -= c.px(kbChipPadL)
		if ch.del != (rect{}) {
			text.Right = ch.del.Left + c.px(2)
		}
		chipR := c.px(7)
		col := colText
		switch ch.style {
		case kbChipKey:
			chipBg := colControl
			if st.hover == seg || st.press == seg {
				chipBg = shade(chipBg, 10)
			}
			c.roundRect(ch.box, chipR, chipBg)
		case kbChipInherited:
			c.outline(ch.box, chipR, colControl)
			col = colMuted
		case kbChipOff:
			c.roundRect(ch.box, chipR, colRowHover)
			col = colMuted
		case kbChipRecording:
			c.frame(ch.box, chipR, shade(bg, -4), colAccent)
			col = colAccent
		}
		c.text(ch.text, text, fontBody, col, dtLeft)
		if ch.del != (rect{}) {
			xCol := colMuted
			if st.hover == seg || st.press == seg {
				xCol = colText
			}
			c.icon(IconClose, rect{ch.del.Left, ch.del.Top, ch.del.Right - c.px(5), ch.del.Bottom}, 12, xCol)
		}
	}
	if l.hint != (rect{}) {
		c.textWrapped(s.Hint, l.hint, fontCaption, colMuted)
	}
	if l.field != (rect{}) {
		f, _ := k.field(kbSegField)
		paintField(c, l.field, f, st.open == kbSegField, st.hover == kbSegField, st.press == kbSegField)
	}
	if l.note != (rect{}) {
		c.textWrapped(s.Note, l.note, fontBody, s.NoteTone.color())
	}
}

// --- Toggle with an info icon ------------------------------------------------

type toggleInfoRow struct {
	toggleRow
	info string
}

// ToggleInfo is a Toggle with an info icon after its label, whose tooltip
// says what the switch does.
func ToggleInfo(label, info string, on bool, onClick func()) Row {
	return toggleInfoRow{toggleRow{label, on, onClick}, info}
}

func (t toggleInfoRow) iconBox(c *canvas, r rect) rect {
	in := inset(c, r)
	x := in.Left + c.textWidth(t.label, fontBody) + c.px(3)
	size := c.px(20)
	top := r.Top + (r.h()-size)/2
	return rect{x, top, x + size, top + size}
}

// Segments: the info icon first (it lies on the switch's row), then the
// switch.
func (t toggleInfoRow) segments(c *canvas, r rect) []rect {
	return []rect{t.iconBox(c, r), t.toggleRow.segments(c, r)[0]}
}
func (t toggleInfoRow) click(seg int) {
	if seg == 1 {
		t.toggleRow.click(0)
	}
}
func (t toggleInfoRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 0 {
		return t.info
	}
	return ""
}
func (t toggleInfoRow) paint(c *canvas, r rect, st rowState) {
	switch1 := func(seg int) int { // the switch is segment 0 of toggleRow
		if seg == 1 {
			return 0
		}
		return -1
	}
	t.toggleRow.paint(c, r, rowState{hover: switch1(st.hover), press: switch1(st.press), open: -1})
	col := colCaption
	if st.hover == 0 {
		col = colText
	}
	c.icon(IconInfo, t.iconBox(c, r), 14, col)
}
