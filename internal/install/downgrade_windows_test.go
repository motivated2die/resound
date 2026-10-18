package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsDowngrade(t *testing.T) {
	exe := filepath.Join(t.TempDir(), ExeName)
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), ExeName)
	for _, c := range []struct {
		installed, version, target string
		want                       bool
	}{
		{"0.15.0", "0.14.2", exe, true},      // the case from the log
		{"0.14.3", "0.15.0", exe, false},     // an update
		{"0.15.0", "0.15.0", exe, false},     // the same version again
		{"", "0.14.2", exe, false},           // a first install
		{"0.15.0", "0.14.2", missing, false}, // the installed copy was deleted
	} {
		if got := isDowngrade(c.installed, c.version, c.target); got != c.want {
			t.Errorf("isDowngrade(%q, %q, exists=%v) = %v, want %v", c.installed, c.version, c.target == exe, got, c.want)
		}
	}
}
