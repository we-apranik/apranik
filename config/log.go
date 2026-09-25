package config

import (
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/logging"
)

func (b *configBuilder) buildLog(raw rawLog, loc string) blueprint.Log {
	var log blueprint.Log

	if raw.Level != "" {
		level, err := logging.ParseLevel(raw.Level)
		if err != nil {
			b.diagnostics.report(fieldLocation(loc, "level"), err)
		} else {
			log.Level = level
		}
	}

	for index, raw := range raw.Outputs {
		output, err := buildOutput(raw)
		if err != nil {
			b.diagnostics.report(indexLocation(fieldLocation(loc, "outputs"), index), err)
			continue
		}
		log.Outputs = append(log.Outputs, output)
	}

	return log
}

// A switch over a closed set the logging system owns -- four outputs -- not
// dispatch over an open set, which is what the registry is for. Nothing is
// opened here; New does that.
func buildOutput(raw rawOutput) (logging.Output, error) {
	switch raw.Type {
	case "stderr":
		return logging.StderrOutput{}, nil
	case "file":
		return logging.FileOutput{Path: raw.Path}, nil
	case "memory":
		return logging.MemoryOutput{Capacity: raw.Capacity}, nil
	case "syslog":
		return logging.SyslogOutput{Network: raw.Network, Address: raw.Address, Tag: raw.Tag}, nil
	case "":
		return nil, errMissingField("type", "log output")
	default:
		return nil, unknownOutputError{raw.Type}
	}
}
