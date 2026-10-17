package server

import (
	"testing"
	"time"
)

func TestDescribeAgent(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36":                            "Chrome on Android",
		"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/28.0 Chrome/130.0.0.0 Mobile Safari/537.36": "Samsung Internet on Android",
		"Mozilla/5.0 (Android 14; Mobile; rv:143.0) Gecko/143.0 Firefox/143.0":                                                                       "Firefox on Android",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Mobile/15E148 Safari/604.1":    "Safari on iPhone",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0.0.0 Mobile/15E148 Safari/604.1": "Chrome on iPhone",
		"Mozilla/5.0 (iPad; CPU OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Mobile/15E148 Safari/604.1":             "Safari on iPad",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0":              "Edge on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Safari/605.1.15":                      "Safari on Mac",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 OPR/122.0.0.0":              "Opera on Windows",
		"Mozilla/5.0 (Linux; Android 14)": "Android device",
		"test":                            "Unknown device",
		"":                                "Unknown device",
	} {
		if got := DescribeAgent(ua); got != want {
			t.Errorf("DescribeAgent(%.40q…) = %q, want %q", ua, got, want)
		}
	}
}

func TestDevicesNewestFirst(t *testing.T) {
	s, _, _ := newTestServer(t)
	a := s.Auth()
	base := time.Now()
	for i, agent := range []string{"old", "new", "middle"} {
		a.now = func() time.Time { return base.Add(time.Duration([]int{0, 2, 1}[i]) * time.Hour) }
		if _, _, err := a.PairDevice(a.PairCode(), agent); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, d := range a.Devices() {
		got = append(got, d.Agent)
	}
	if len(got) != 3 || got[0] != "new" || got[1] != "middle" || got[2] != "old" {
		t.Fatalf("devices = %v", got)
	}
}
