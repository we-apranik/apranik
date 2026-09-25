package config

import (
	"bytes"
	"encoding/json"

	"github.com/The-Apranik/Apranik/spec"
)

// Strict unless the config file opted out: a silently ignored field ships
// behaviour the user never asked for.
//
// Validate runs here; Warnings does not. Check collects those while it walks
// the whole config -- doing both would report each twice.
func (b *configBuilder) decodeSettings(target spec.Config, raw json.RawMessage, loc string) bool {
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if b.strict {
			decoder.DisallowUnknownFields()
		}
		if err := decoder.Decode(target); err != nil {
			b.diagnostics.report(loc, err)
			return false
		}
	}

	if err := target.Validate(); err != nil {
		b.diagnostics.report(loc, err)
		return false
	}
	return true
}
