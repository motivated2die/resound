package importer

import (
	"context"
	"os"
	"testing"

	"github.com/motivated2die/resound/internal/audio"
)

// TestLiveMyinstants downloads a real sound with the guarded client and
// the real decoder check. It needs the internet: RESOUND_LIVE=1.
func TestLiveMyinstants(t *testing.T) {
	if os.Getenv("RESOUND_LIVE") != "1" {
		t.Skip("set RESOUND_LIVE=1 to fetch from myinstants.com")
	}
	f := New(audio.SupportedExtensions, audio.CheckFile)
	dir := t.TempDir()
	for _, link := range []string{
		"https://www.myinstants.com/en/instant/vine-boom-sound-70972/",
		"https://www.myinstants.com/en/instant/fahhhhhhhhhhhhhh-3525/",
		"https://www.myinstants.com/media/sounds/vine-boom.mp3",
	} {
		got, err := f.Fetch(context.Background(), link, dir)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		t.Logf("%s -> %s", link, got.Path)
	}
	if _, err := f.Fetch(context.Background(), "https://www.myinstants.com/en/search/?name=vine", dir); err != ErrNoSound {
		t.Errorf("search page: err = %v, want ErrNoSound", err)
	}
}
