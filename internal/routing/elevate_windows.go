package routing

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/motivated2die/resound/internal/elevate"
	"github.com/motivated2die/resound/internal/logx"
)

// ApplyCommand is the first argument that makes the executable run as the
// short-lived elevated routing helper instead of the tray app.
const ApplyCommand = "--apply-routing"

// ErrCancelled means the user declined the UAC prompt.
var ErrCancelled = elevate.ErrCancelled

// Exit codes of the helper process.
const (
	exitOK = iota
	exitFailed
	exitBadArgs
	exitAccessDenied
)

// ApplyElevated starts this executable elevated ("runas") to switch
// "Listen to this device" on inputID, playing through targetID. It blocks
// until the helper exits, so call it off the UI thread.
func ApplyElevated(inputID, targetID string, enable bool) error {
	// Endpoint IDs contain only braces, dots, digits and hex letters, so
	// plain quoting is safe.
	params := fmt.Sprintf(`%s "--input=%s" "--target=%s" --enable=%v`, ApplyCommand, inputID, targetID, enable)
	logx.Printf("routing: requesting elevation: %s", params)
	code, err := elevate.Run(params, 60*time.Second)
	if err != nil {
		return err
	}
	switch code {
	case exitOK:
		return nil
	case exitAccessDenied:
		return ErrAccessDenied
	case exitBadArgs:
		return errors.New("elevated helper rejected its arguments (see log)")
	default:
		return fmt.Errorf("elevated helper failed with code %d (see log)", code)
	}
}

// RunApplyCommand is the entry point of the elevated helper. args are the
// arguments after ApplyCommand. It returns the process exit code.
func RunApplyCommand(args []string) int {
	fs := flag.NewFlagSet(ApplyCommand, flag.ContinueOnError)
	input := fs.String("input", "", "capture endpoint ID")
	target := fs.String("target", "", "render endpoint ID to play through")
	enable := fs.Bool("enable", true, "turn listening on (true) or off (false)")
	if err := fs.Parse(args); err != nil || *input == "" || (*enable && *target == "") ||
		!looksLikeEndpointID(*input) || (*target != "" && !looksLikeEndpointID(*target)) {
		logx.Printf("routing helper: bad arguments %q", args)
		return exitBadArgs
	}
	logx.Printf("routing helper: input=%s target=%s enable=%v", *input, *target, *enable)
	if err := apply(*input, *target, *enable); err != nil {
		logx.Printf("routing helper: %v", err)
		if errors.Is(err, ErrAccessDenied) {
			return exitAccessDenied
		}
		return exitFailed
	}
	logx.Printf("routing helper: done")
	return exitOK
}

// looksLikeEndpointID accepts "{0.0.0.00000000}.{guid}"-style IDs only, so
// the helper can't be pointed at anything else.
func looksLikeEndpointID(s string) bool {
	if !strings.HasPrefix(s, "{") || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F',
			r == '{', r == '}', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
}
