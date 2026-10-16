package ui

import "testing"

func TestIconsRender(t *testing.T) {
	names := iconNames()
	if len(names) == 0 {
		t.Fatal("no embedded icons")
	}
	have := map[Icon]bool{}
	for _, name := range names {
		have[name] = true
		for _, size := range []int{16, 20, 24, 30} {
			mask, err := rasterIcon(name, size)
			if err != nil {
				t.Fatal(err)
			}
			opaque, partial := 0, 0
			for _, a := range mask.Pix {
				switch {
				case a == 255:
					opaque++
				case a > 0:
					partial++
				}
			}
			// Anti-aliased: edges are partly covered, and the icon isn't
			// a filled square (e.g. Tabler's invisible bounding path).
			if opaque+partial == 0 || partial == 0 || opaque+partial == size*size {
				t.Errorf("%s at %d px: %d opaque, %d partial pixels", name, size, opaque, partial)
			}
		}
	}
	for _, name := range []Icon{IconAlert, IconChevronLeft, IconClose, IconChevronRight, IconDownload, IconExternalLink, IconFolder, IconFolderPlus, IconHandGrab, IconHelp, IconTrash, IconInfo, IconKeyboard, IconMic, IconMicOff, IconPause, IconPlay, IconPower, IconRefresh, IconRestore, IconSave, IconSettings, IconStop, IconUnplugged} {
		if !have[name] {
			t.Errorf("constant %q has no icons/%s.svg", name, name)
		}
	}
}

func TestIconKeepsAspect(t *testing.T) {
	// A filled 2:1 view box (like many Font Awesome icons) must be centred
	// vertically, not stretched to the square.
	wide := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 320"><path d="M0 0h640v320H0z"/></svg>`)
	mask, err := rasterSVG(wide, 40)
	if err != nil {
		t.Fatal(err)
	}
	if mask.AlphaAt(20, 5).A != 0 || mask.AlphaAt(20, 34).A != 0 || mask.AlphaAt(20, 20).A != 255 {
		t.Fatalf("rows 5/20/34 = %d/%d/%d, want 0/255/0", mask.AlphaAt(20, 5).A, mask.AlphaAt(20, 20).A, mask.AlphaAt(20, 34).A)
	}
}
