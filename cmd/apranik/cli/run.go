package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/we-apranik/apranik/config"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/logging/sink"
	"github.com/we-apranik/apranik/core/registry"
)

const defaultConfigSource = "config.json"

var cmdRun = &Command{
	Name:  "run",
	Short: "load a config file and serve it",
	Usage: `Usage: apranik run [-config source] [-fetch-timeout duration]

Load the config file, check it, build the inbounds and outbounds
it describes, and serve until interrupted. SIGHUP (and an API that offers
it) loads the config again: a config with errors is reported and the running
one keeps serving, otherwise everything is closed and started again from the
new one, with the statistics counting from zero.

Flags:
  -config source          file path or http(s) URL of the config file (default "config.json")
  -fetch-timeout duration  how long to wait for a config file given as a URL (default 30s)`,
	Run: runRun,
}

func runRun(args []string) error {
	flags := flag.NewFlagSet("apranik run", flag.ContinueOnError)
	source := flags.String("config", defaultConfigSource, "file path or URL of the config file")
	fetchTimeout := flags.Duration("fetch-timeout", config.DefaultFetchTimeout, "how long to wait for a config file given as a URL")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q; name the config file with -config", flags.Arg(0))
	}

	coreBlueprint, diagnostics, err := load(*source, *fetchTimeout)
	if err != nil {
		return err
	}

	logger := newBootstrapLogger(coreBlueprint)
	report(logger, diagnostics)
	if diagnostics.HasErrors() {
		return logFailure(logger, "Config has errors", "source", *source)
	}

	if err := serve(*source, *fetchTimeout, coreBlueprint); err != nil {
		return logFailure(logger, "Could not start", "error", err)
	}
	return nil
}

// Both run and check go through it. Only http and https name a URL, so a
// Windows path is not mistaken for a scheme.
func load(source string, fetchTimeout time.Duration) (*blueprint.Blueprint, config.Diagnostics, error) {
	modules, err := registry.Default()
	if err != nil {
		return nil, nil, fmt.Errorf("building the module registry: %w", err)
	}

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		coreBlueprint, diagnostics := config.LoadURL(ctx, source, modules)
		return coreBlueprint, diagnostics, nil
	}
	coreBlueprint, diagnostics := config.LoadFile(source, modules)
	return coreBlueprint, diagnostics, nil
}

// newBootstrapLogger logs until the configured logger exists: always to
// stderr, since the config may be unreadable or silence the log, and in the
// format of the config's stderr output when it has a valid one.
func newBootstrapLogger(coreBlueprint *blueprint.Blueprint) *logging.Logger {
	var stderr sink.StderrOutput
	if coreBlueprint != nil {
		for _, output := range coreBlueprint.Log.Outputs {
			if configured, ok := output.(sink.StderrOutput); ok && configured.Validate() == nil {
				stderr = configured
			}
		}
	}
	system, err := logging.New(logging.Config{
		Level:   logging.LevelDebug,
		Outputs: []logging.Output{stderr},
	})
	if err != nil {
		return logging.Discard()
	}
	return system.Logger()
}

// ErrReported means the command has already logged why it failed, so main
// only sets the exit status.
var ErrReported = errors.New("failure already reported")

func logFailure(logger *logging.Logger, message string, attributes ...any) error {
	logger.Error(message, attributes...)
	return ErrReported
}

// Warnings first, so the last line is what stopped the run.
func report(logger *logging.Logger, diagnostics config.Diagnostics) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == config.SeverityWarning {
			logger.Warn("Config warning", diagnosticAttributes(diagnostic)...)
		}
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == config.SeverityError {
			logger.Error("Config error", diagnosticAttributes(diagnostic)...)
		}
	}
}

func diagnosticAttributes(diagnostic config.Diagnostic) []any {
	if diagnostic.Location == "" {
		return []any{"problem", diagnostic.Message}
	}
	return []any{"location", diagnostic.Location, "problem", diagnostic.Message}
}

// valueOrZero reads a setting that was left absent as zero, which the handler
// reads as its default.
func valueOrZero[T any](setting *T) T {
	var zero T
	if setting == nil {
		return zero
	}
	return *setting
}
