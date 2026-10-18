package ui

// The sound list of an opened folder on the library page: one slim row per
// sound, with small icon buttons to manage it.

// Tool is a small icon button on a SoundItem row.
type Tool struct {
	Icon    Icon
	Tip     string
	Danger  bool // its icon turns the destructive colour under the pointer
	OnClick func()
}

// SoundSpec describes one SoundItem row.
type SoundSpec struct {
	Name string
	Tip  string // tooltip on the name, e.g. the full path
	// Depth indents the row like a subfolder of that depth: the folder's
	// own depth + 1.
	Depth int
	// Note, in the accent colour, takes the place of the tools, e.g.
	// "Editing in your browser…".
	Note string
	// Muted draws the name as a placeholder ("No sounds in this folder").
	Muted bool
	Tools []Tool
}

type soundItemRow struct{ s SoundSpec }

// SoundItem is a sound in a folder's list: a small speaker, its name, and
// its tools on the right.
func SoundItem(s SoundSpec) Row { return soundItemRow{s} }

const soundToolL = 24 // a tool's size

func (soundItemRow) height() int     { return 28 }
func (soundItemRow) keepsOpen() bool { return true }

// geometry returns the row's band (indented like a subfolder's field) and
// the tools' squares, left to right.
func (s soundItemRow) geometry(c *canvas, r rect) (band rect, tools []rect) {
	in := inset(c, r)
	band = rect{in.Left + c.px(folderIndentL*min(s.s.Depth, folderMaxIndent)), r.Top + c.px(2), in.Right, r.Bottom - c.px(2)}
	if s.s.Note != "" || s.s.Muted {
		return band, nil
	}
	size := min(c.px(soundToolL), band.h())
	right := band.Right - c.px(2)
	for range s.s.Tools {
		tools = append(tools, rect{right - size, band.Top + (band.h()-size)/2, right, band.Top + (band.h()+size)/2})
		right -= size + c.px(2)
	}
	// Built right to left: the first tool sits leftmost.
	for i, j := 0, len(tools)-1; i < j; i, j = i+1, j-1 {
		tools[i], tools[j] = tools[j], tools[i]
	}
	return band, tools
}

// Segment 0 is the name (hover only), then one per tool.
func (s soundItemRow) segments(c *canvas, r rect) []rect {
	band, tools := s.geometry(c, r)
	name := band
	if len(tools) > 0 {
		name.Right = tools[0].Left
	}
	if s.s.Muted {
		name = rect{}
	}
	return append([]rect{name}, tools...)
}

func (s soundItemRow) click(seg int) {
	if seg >= 1 && seg <= len(s.s.Tools) && s.s.Tools[seg-1].OnClick != nil {
		s.s.Tools[seg-1].OnClick()
	}
}

func (s soundItemRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 0 {
		return s.s.Tip
	}
	if seg <= len(s.s.Tools) {
		return s.s.Tools[seg-1].Tip
	}
	return ""
}

func (s soundItemRow) paint(c *canvas, r rect, st rowState) {
	band, tools := s.geometry(c, r)
	if st.hover >= 0 {
		c.roundRect(band, c.px(6), colRowHover)
	}
	pad := c.px(10)
	iconBox := rect{band.Left + pad, band.Top, band.Left + pad + c.px(16), band.Bottom}
	nameCol := colText
	if s.s.Muted {
		nameCol = colCaption
	} else {
		c.icon(IconVolume, iconBox, 11, colCaption)
	}
	nameRight := band.Right - pad
	if len(tools) > 0 {
		nameRight = tools[0].Left - c.px(6)
	}
	if s.s.Note != "" {
		w := min(c.textWidth(s.s.Note, fontCaption)+c.px(2), band.w()/2)
		c.text(s.s.Note, rect{band.Right - pad - w, band.Top, band.Right - pad, band.Bottom}, fontCaption, colAccent, dtRight)
		nameRight = band.Right - pad - w - c.px(8)
	}
	c.text(s.s.Name, rect{iconBox.Right + c.px(8), band.Top, nameRight, band.Bottom}, fontBody, nameCol, dtLeft)

	for i, t := range tools {
		tool := s.s.Tools[i]
		col := colCaption
		switch {
		case st.press == i+1:
			c.roundRect(t, c.px(6), colRowPress)
			col = colText
		case st.hover == i+1:
			c.roundRect(t, c.px(6), colRowPress)
			col = colText
		case st.hover >= 0:
			col = colMuted // the row is under the pointer: its tools stand out a little
		}
		if tool.Danger && (st.hover == i+1 || st.press == i+1) {
			col = colDanger
		}
		c.icon(tool.Icon, t, 14, col)
	}
}
