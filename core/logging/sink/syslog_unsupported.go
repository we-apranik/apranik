//go:build windows || plan9

package sink

import (
	"errors"
	"log/slog"
	"runtime"
)

// log/syslog does not build on this platform, so the sink reports that at
// startup rather than the whole project failing to compile for it.
func NewSyslog(network, address, tag string, level slog.Level) (slog.Handler, error) {
	return nil, errors.New("logging: syslog sink is not available on " + runtime.GOOS)
}
