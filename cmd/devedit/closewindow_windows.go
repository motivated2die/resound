package main

import (
	"log"
	"time"

	"github.com/motivated2die/resound/internal/ui"
)

// closeWindow closes the page's app window when it finished, as the panel
// does.
func closeWindow(title string) {
	if !ui.CloseBrowserWindow(title, 2*time.Second) {
		log.Printf("no app window titled %q: the page asks to close it", title)
	}
}
