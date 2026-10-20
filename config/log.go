package config

import (
	"fmt"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/logging/sink"
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

func buildOutput(raw rawOutput) (logging.Output, error) {
	switch raw.Type {
	case "stderr":
		return sink.StderrOutput{Format: sink.Format(raw.Format), Color: raw.Color}, nil
	case "file":
		return sink.FileOutput{Path: raw.Path, Format: sink.Format(raw.Format)}, nil
	case "memory":
		if err := types.CheckPositive(raw.Capacity); err != nil {
			return nil, fmt.Errorf("capacity %w", err)
		}
		return sink.MemoryOutput{Capacity: types.ValueOrDefault(raw.Capacity, 0)}, nil
	case "syslog":
		return sink.SyslogOutput{Network: raw.Network, Address: raw.Address, Tag: raw.Tag}, nil
	case "":
		return nil, errMissingField("type", "log output")
	default:
		return nil, unknownOutputError{raw.Type}
	}
}
