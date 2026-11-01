package audio

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/motivated2die/resound/internal/wincom"
)

func TestProbeToneStaysQuietAndFades(t *testing.T) {
	pcm := probeTone(48000)
	if abs := math.Abs(float64(pcm[0])); abs > 0.001 {
		t.Errorf("starts at %v: it would click", abs)
	}
	if abs := math.Abs(float64(pcm[len(pcm)-1])); abs > 0.001 {
		t.Errorf("ends at %v: it would click", abs)
	}
	var peak float32
	for _, v := range pcm {
		peak = max(peak, v, -v)
	}
	if peak > probeLevel || peak < probeFloor {
		t.Errorf("peak %v, want between the meter's floor %v and the level %v", peak, probeFloor, probeLevel)
	}
}

// TestProbeCableLive plays the quiet tone into the real cable, which apps
// recording CABLE Output would get (opt-in: RESOUND_LIVE_CABLE=1).
func TestProbeCableLive(t *testing.T) {
	if os.Getenv("RESOUND_LIVE_CABLE") != "1" {
		t.Skip("RESOUND_LIVE_CABLE not set")
	}
	var input, output string
	err := wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		outs, err := wincom.Enumerate(e, wincom.Render)
		if err != nil {
			return err
		}
		for _, o := range outs {
			if strings.HasPrefix(strings.ToLower(o.Name), "cable input") {
				input = o.ID
			}
		}
		ins, err := wincom.Enumerate(e, wincom.Capture)
		if err != nil {
			return err
		}
		for _, o := range ins {
			if strings.HasPrefix(strings.ToLower(o.Name), "cable output") {
				output = o.ID
			}
		}
		return nil
	})
	if err != nil || input == "" || output == "" {
		t.Skipf("no VB-Cable: %v", err)
	}
	engine := NewEngine()
	defer engine.Close()
	if err := engine.SetOutput(RoleCable, input, true); err != nil {
		t.Fatal(err)
	}
	got, err := engine.ProbeCable(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != ProbePassed {
		t.Errorf("probe = %d, want passed", got)
	}
}
