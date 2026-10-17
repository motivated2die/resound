package routing

import "testing"

func TestLooksLikeEndpointID(t *testing.T) {
	good := []string{
		"{0.0.0.00000000}.{6ff3afb9-1d07-46fd-b5db-257004dccf4f}",
		"{0.0.1.00000000}.{8DD8D8C0-A7C6-467C-966D-1FB4B1BB172C}",
	}
	bad := []string{"", "cmd.exe", `{0.0.0.00000000}.{x}" --evil`, "{a} {b}", "C:\\Windows"}
	for _, s := range good {
		if !looksLikeEndpointID(s) {
			t.Errorf("rejected valid ID %q", s)
		}
	}
	for _, s := range bad {
		if looksLikeEndpointID(s) {
			t.Errorf("accepted invalid ID %q", s)
		}
	}
}

func TestPickMicSkipsCable(t *testing.T) {
	inputs := []Input{
		{ID: "cable", Name: "CABLE Output (VB-Audio Virtual Cable)", Default: true},
		{ID: "mic", Name: "Mikrofon (Yealink BT51)"},
	}
	if got := pickMic(inputs, "", ""); got == nil || got.ID != "mic" {
		t.Fatalf("auto pick = %+v, want the real mic", got)
	}
	if got := pickMic(inputs, "gone", ""); got != nil {
		t.Fatalf("missing chosen mic should give nil, got %+v", got)
	}
}

func TestPickMicSkipsMixerOutputs(t *testing.T) {
	inputs := []Input{
		{ID: "b1", Name: "Voicemeeter Out B1 (VB-Audio Voicemeeter VAIO)", Default: true},
		{ID: "sonar", Name: "SteelSeries Sonar - Microphone (SteelSeries Sonar Virtual Audio Device)"},
		{ID: "mic", Name: "Mikrofon (BlackShark V3 Pro XBOX - Chat)"},
	}
	if got := pickMic(inputs, "", ""); got == nil || got.ID != "sonar" {
		t.Fatalf("auto pick = %+v, want the first device that isn't Voicemeeter's", got)
	}
	if got := pickMic(inputs, "", "sonar"); got == nil || got.ID != "mic" {
		t.Fatalf("auto pick = %+v, want to skip the apps' microphone", got)
	}
	if got := pickMic(inputs, "b1", "sonar"); got == nil || got.ID != "b1" {
		t.Fatalf("a chosen mic is kept, got %+v", got)
	}
}
