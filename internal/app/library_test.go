package app

import (
	"testing"

	"github.com/motivated2die/resound/internal/config"
)

func TestFolderCovering(t *testing.T) {
	a := &App{cfg: config.Config{Folders: []string{`D:\Sounds`, `E:\Clips\`}}}
	for path, want := range map[string]bool{
		`D:\Sounds`:           true, // already a folder
		`d:\sounds`:           true, // case-insensitive
		`E:\Clips`:            true, // trailing separator in the config
		`D:\Sounds\Memes`:     true, // subfolders are scanned with it
		`D:\Sounds\Memes\Old`: true, // at any depth
		`D:\Other`:            false,
		`D:\Sounds 2`:         false, // same prefix, different folder
		`D:\`:                 false, // a parent becomes its own folder
	} {
		if got := a.folderCovering(path); got != want {
			t.Errorf("folderCovering(%q) = %v, want %v", path, got, want)
		}
	}
}
