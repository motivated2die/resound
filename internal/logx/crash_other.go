//go:build !windows

package logx

import (
	"errors"
	"time"
)

func crashEvents(int, time.Time) ([]crashEvent, error) {
	return nil, errors.New("not available on this system")
}
