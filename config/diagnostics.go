package config

import (
	"errors"
	"fmt"
	"strings"
)

type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

func (s Severity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return "error"
}

type Diagnostic struct {
	Location string // "outbounds[0].transport[1].settings"
	Severity Severity
	Message  string
}

func (d Diagnostic) String() string {
	if d.Location == "" {
		return fmt.Sprintf("%s: %s", d.Severity, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", d.Severity, d.Location, d.Message)
}

type Diagnostics []Diagnostic

func (d Diagnostics) HasErrors() bool {
	for _, diagnostic := range d {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (d Diagnostics) Err() error {
	var failures []string
	for _, diagnostic := range d {
		if diagnostic.Severity == SeverityError {
			failures = append(failures, diagnostic.String())
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New(strings.Join(failures, "\n"))
}

// Pointer receiver so a helper can append
func (d *Diagnostics) errorf(loc, format string, args ...any) {
	*d = append(*d, Diagnostic{
		Location: loc, Severity: SeverityError, Message: fmt.Sprintf(format, args...),
	})
}

func (d *Diagnostics) report(loc string, err error) {
	d.errorf(loc, "%v", err)
}

func (d *Diagnostics) warnf(loc, format string, args ...any) {
	*d = append(*d, Diagnostic{
		Location: loc, Severity: SeverityWarning, Message: fmt.Sprintf(format, args...),
	})
}
