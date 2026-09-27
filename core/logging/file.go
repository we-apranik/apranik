package logging

import (
	"fmt"
	"log/slog"
	"os"
)

// NewFileSink appends records to path in format. Opt in deliberately, never by
// default: a log file outlives the process, and on a seized device it is
// evidence about the person who ran it.
func NewFileSink(path string, level Level, format Format) (slog.Handler, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("logging: open log file %s: %w", path, err)
	}

	return &fileSink{
		Handler: newFormatHandler(file, format, level, false),
		file:    file,
	}, nil
}

type fileSink struct {
	slog.Handler
	file *os.File
}

func (s *fileSink) Close() error { return s.file.Close() }
