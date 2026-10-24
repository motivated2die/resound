package ui

import "testing"

func testTrayMenu(scale float64) *trayMenu {
	m := &trayMenu{scale: scale, hover: -1, press: -1, actions: [3]Action{
		{Label: "Show panel"},
		{Label: "Quit", Icon: IconPower, Style: ButtonDanger},
		{Label: "Reset panel position", Icon: IconRestore},
	}}
	c := m.canvas(0)
	m.w, m.h = c.px(trayMenuWL), trayMenuHeight(c)
	return m
}

// TestTrayMenuLayout checks the two-thirds / one-third split, the full-width
// reset button below, and that clicks between and around the buttons hit
// nothing.
func TestTrayMenuLayout(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		m := testTrayMenu(scale)
		b := m.boxes(m.canvas(0))
		show, quit, reset := b[0], b[1], b[2]
		if d := show.w() - 2*quit.w(); d < -2 || d > 2 {
			t.Errorf("scale %v: Show panel %d px, Quit %d px; want 2:1", scale, show.w(), quit.w())
		}
		if quit.Right != m.w-m.canvas(0).px(trayMenuPadL) || show.Left != m.canvas(0).px(trayMenuPadL) {
			t.Errorf("scale %v: buttons %v %v don't fill the menu's width %d", scale, show, quit, m.w)
		}
		if reset.Left != show.Left || reset.Right != quit.Right || reset.Bottom != m.h-m.canvas(0).px(trayMenuPadL) {
			t.Errorf("scale %v: reset %v isn't the full-width bottom row of a %d×%d menu", scale, reset, m.w, m.h)
		}
		cy := show.Top + show.h()/2
		for _, tc := range []struct {
			x, y int32
			want int
		}{
			{show.Left + 1, cy, 0}, {quit.Right - 1, cy, 1},
			{(show.Right + quit.Left) / 2, cy, -1}, // the gap
			{show.Left + 1, 0, -1},                 // the padding
			{reset.Right - 1, reset.Bottom - 1, 2},
			{show.Left + 1, (show.Bottom + reset.Top) / 2, -1}, // between the rows
		} {
			if got := m.hit(tc.x, tc.y); got != tc.want {
				t.Errorf("scale %v: hit(%d, %d) = %d, want %d", scale, tc.x, tc.y, got, tc.want)
			}
		}
	}
}

// TestTrayMenuRender saves the menu, plain and with Quit hovered, and
// checks that each label fits beside its icon.
func TestTrayMenuRender(t *testing.T) {
	for _, scale := range []float64{1, 1.5} {
		for _, hover := range []int{-1, 1} {
			m := testTrayMenu(scale)
			m.hover = hover
			img := renderCanvas(t, m.w, m.h, scale, func(c *canvas) {
				m.fonts = c.fonts
				m.draw(c)
				for i, b := range m.boxes(c) {
					label := m.actions[i].Label
					if need := c.px(buttonIconL) + c.px(7) + c.textWidth(label, fontBold) + 2*c.px(10); need > b.w() {
						t.Errorf("scale %v: %s needs %d px, has %d", scale, label, need, b.w())
					}
				}
			})
			name := "tray-menu"
			if hover >= 0 {
				name += "-hover"
			}
			if scale != 1 {
				name += "@1.5x"
			}
			savePNG(t, img, name)
		}
	}
}
