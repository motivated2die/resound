package importer

import (
	"context"
	"os"
	"testing"

	"github.com/motivated2die/resound/internal/audio"
)

// TestLiveSites downloads real sounds from Myinstants and Sound Buttons
// World with the guarded client and the real decoder check, and checks
// that their list pages are refused. It needs the internet: RESOUND_LIVE=1.
func TestLiveSites(t *testing.T) {
	if os.Getenv("RESOUND_LIVE") != "1" {
		t.Skip("set RESOUND_LIVE=1 to fetch from myinstants.com and soundbuttonsworld.com")
	}
	f := New(audio.SupportedExtensions, audio.CheckFile)
	dir := t.TempDir()
	for _, link := range []string{
		"https://www.myinstants.com/en/instant/vine-boom-sound-70972/",
		"https://www.myinstants.com/en/instant/fahhhhhhhhhhhhhh-3525/",
		"https://www.myinstants.com/media/sounds/vine-boom.mp3",
		"https://soundbuttonsworld.com/sound-button/67-67-67-1739753521222",
	} {
		got, err := f.Fetch(context.Background(), link, dir)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		t.Logf("%s -> %s", link, got.Path)
	}
	for _, list := range []string{
		"https://www.myinstants.com/en/search/?name=vine",
		"https://soundbuttonsworld.com/categories/memes%20soundboard",
	} {
		if _, err := f.Fetch(context.Background(), list, dir); err != ErrNoSound {
			t.Errorf("%s: err = %v, want ErrNoSound", list, err)
		}
	}
}
