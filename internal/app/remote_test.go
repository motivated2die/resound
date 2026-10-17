package app

import (
	"testing"
	"time"
)

func TestDayText(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 30, 0, 0, time.Local)
	for _, tc := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-time.Hour), "today"},
		{time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local), "today"},
		{time.Date(2026, 10, 16, 23, 59, 0, 0, time.Local), "yesterday"},
		{time.Date(2026, 10, 16, 0, 1, 0, 0, time.Local), "yesterday"},
		{time.Date(2026, 10, 15, 23, 0, 0, 0, time.Local), "2 days ago"},
		{time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local), "6 days ago"},
		{time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local), "on 10 Oct"},
		{time.Date(2025, 12, 24, 12, 0, 0, 0, time.Local), "on 24 Dec 2025"},
	} {
		if got := dayText(tc.t, now); got != tc.want {
			t.Errorf("dayText(%s) = %q, want %q", tc.t.Format(time.DateTime), got, tc.want)
		}
	}
}
