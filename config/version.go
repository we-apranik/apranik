package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/we-apranik/apranik/core/blueprint"
)

func (b *configBuilder) checkVersions(envelope envelope) bool {
	if envelope.MinCore != "" {
		required, err := parseVersion(envelope.MinCore)
		if err != nil {
			b.diagnostics.report("min_core", err)
			return false
		}
		running, err := parseVersion(blueprint.Version)
		if err != nil {
			b.diagnostics.errorf("min_core", "this build reports an unparsable version %q", blueprint.Version)
			return false
		}
		if running.olderThan(required) {
			b.diagnostics.errorf("min_core",
				"this config requires Apranik >=%s; this build is %s", required, running)
			return false
		}
	}

	if envelope.Version == nil {
		b.diagnostics.errorf("version", "a config must declare its schema version; this core supports %d", blueprint.SchemaVersion)
		return false
	}
	if *envelope.Version != blueprint.SchemaVersion {
		b.diagnostics.errorf("version",
			"config version %d is not supported by this core (supports: %d)", *envelope.Version, blueprint.SchemaVersion)
		return false
	}
	return true
}

type version [3]int

func parseVersion(text string) (version, error) {
	fields := strings.Split(text, ".")
	if len(fields) != 3 {
		return version{}, fmt.Errorf("%q is not a three-part version such as 0.4.0", text)
	}

	var parsed version
	for index, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil || number < 0 {
			return version{}, fmt.Errorf("%q is not a three-part version such as 0.4.0", text)
		}
		parsed[index] = number
	}
	return parsed, nil
}

func (v version) olderThan(right version) bool {
	for index := range v {
		if v[index] != right[index] {
			return v[index] < right[index]
		}
	}
	return false
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}
