package apicoreprovider

import (
	"log/slog"
	"path/filepath"

	"github.com/The-Apranik/Apranik/core/logging/sink"
	"github.com/The-Apranik/Apranik/spec"
)

func (p *Provider) SubscribeToLog(minimumLevel slog.Level, bufferedRecords int) (spec.APILogSubscription, error) {
	subscription, err := p.logs(minimumLevel, bufferedRecords)
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

// LogFilePath is the n-th file output in the configuration, made absolute the
// way the log system opens it: a relative path is relative to the working
// directory.
func (p *Provider) LogFilePath(outputIndex int) (string, error) {
	index := 0
	for _, output := range p.blueprint.Log.Outputs {
		file, isFile := output.(sink.FileOutput)
		if !isFile {
			continue
		}
		if index == outputIndex {
			return filepath.Abs(file.Path)
		}
		index++
	}
	return "", spec.ErrAPINoLogFileOutput
}
