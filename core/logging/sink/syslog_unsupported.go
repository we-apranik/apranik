//go:build windows || plan9

package sink

import (
	"errors"
	"log/slog"
	"runtime"
)

func NewSyslog(network, address, tag string, level slog.Level) (slog.Handler, error) {
	return nil, errors.New("logging: syslog sink is not available on " + runtime.GOOS)
}
