package app

import "testing"

func TestWithin(t *testing.T) {
	for _, tc := range []struct {
		path, root string
		want       bool
	}{
		{`D:\Sounds`, `D:\Sounds`, true},
		{`D:\Sounds\Memes\Anime`, `D:\Sounds`, true},
		{`d:\sounds\memes`, `D:\Sounds\`, true},
		{`D:\Sounds2`, `D:\Sounds`, false},
		{`D:\Other`, `D:\Sounds`, false},
		{`D:\`, `D:\Sounds`, false},
		{`E:\Sounds`, `D:\Sounds`, false},
		{`D:\Sounds\..dots`, `D:\Sounds`, true},
	} {
		if got := within(tc.path, tc.root); got != tc.want {
			t.Errorf("within(%q, %q) = %v, want %v", tc.path, tc.root, got, tc.want)
		}
	}
}
