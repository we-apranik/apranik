package cli

import (
	"flag"

	"fmt"

	"github.com/The-Apranik/Apranik/config"
)

var cmdCheck = &Command{
	Name:  "check",
	Short: "check a config file without running it",
	Usage: `Usage: apranik check [-config source] [-fetch-timeout duration]

Load and check the config file, print what is wrong with it, and
exit non-zero if anything is. Nothing is built and no socket is opened, so a
config file can be checked on a machine that could never run it.

Flags:
  -config source          file path or http(s) URL of the config file (default "config.json")
  -fetch-timeout duration  how long to wait for a config file given as a URL (default 30s)`,
	Run: runCheck,
}

func runCheck(args []string) error {
	flags := flag.NewFlagSet("apranik check", flag.ContinueOnError)
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

	logger.Info("Config is valid", "source", *source)
	return nil
}
