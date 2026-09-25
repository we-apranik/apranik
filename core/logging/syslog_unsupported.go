//go:build windows || plan9

package logging

import (
	"errors"
	"log/slog"
	"runtime"
)

// log/syslog does not build on this platform, so the sink reports that at
// startup rather than the whole project failing to compile for it.
func NewSyslogSink(network, address, tag string, level Level) (slog.Handler, error) {
	return nil, errors.New("logging: syslog sink is not available on " + runtime.GOOS)
}
