//go:build !windows

package main

import "log"

// closeWindow only says what the panel would do: closing windows is Windows
// only.
func closeWindow(title string) {
	log.Printf("the page finished; Resound would close the window %q", title)
}
