package app

import (
	"slices"
	"testing"

	"github.com/motivated2die/resound/internal/audio"
)

// With sounds not overlapping, a new sound takes the place of the one
// playing (which needs no output here: only the bookkeeping counts).
// Push-to-talk stays down across the switch, also when the replaced
// voice's EventFinished arrives afterwards, and goes up once the new sound
// ends.
func TestReplaceKeepsPushToTalk(t *testing.T) {
	ta := newTalkApp(t)
	ta.cfg.NoSoundOverlap = true
	pb := &ta.playback
	finish := func(id uint64, outputs int) {
		for range outputs {
			ta.onEngineEvent(audio.Event{Kind: audio.EventFinished, VoiceID: id})
		}
	}

	// Sound 1 plays on two outputs, paused by the play/pause toggle.
	ta.holdPlayback(false)
	pb.remaining[1] = 2
	pb.setVoice(1, voiceMeta{soundID: 10})
	pb.paused, pb.pausedActive = []uint64{1}, true
	if got := ta.sentSince(); got != "+Pause" {
		t.Fatalf("first sound: %q", got)
	}

	// Sound 2 starts, as start does it: held first, then the others go.
	ta.holdPlayback(false)
	if got := ta.replaceVoices(2); !slices.Equal(got, []uint64{1}) {
		t.Fatalf("replaced %v, want [1]", got)
	}
	pb.remaining[2] = 2
	if _, ok := pb.voice(1); ok || pb.pausedActive {
		t.Fatal("the replaced sound or its pause is still known")
	}
	finish(1, 2) // late events of the replaced sound
	if got := ta.sentSince(); got != "" || ta.ptt.pressed == nil {
		t.Fatalf("across the switch: sent %q, pressed %v", got, ta.ptt.pressed)
	}

	finish(2, 2)
	if got := ta.sentSince(); got != "-Pause" {
		t.Fatalf("after the new sound: %q", got)
	}
}
