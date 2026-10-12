// Command resound is the Windows tray host of the Resound soundboard.
package main

import (
	"os"

	"github.com/motivated2die/resound/internal/app"
)

func main() {
	os.Exit(app.Run())
}
