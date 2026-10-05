package cli

import (
	"flag"

	"fmt"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/builder"
)

var cmdCheck = &Command{
	Name:  "check",
	Short: "check a config file without running it",
	Usage: `Usage: apranik check [-config source] [-fetch-timeout duration] [-ports]

Load and check the config file, build everything it describes without
dialing anything, print what is wrong, and exit non-zero if anything is.
Nothing is bound unless -ports is given.

Flags:
  -config source          file path or http(s) URL of the config file (default "config.json")
  -fetch-timeout duration  how long to wait for a config file given as a URL (default 30s)
  -ports                  also bind every inbound's address and release it, to find a port
                          already in use or given to two inbounds; leave it off while the
                          process this config replaces is still running`,
	Run: runCheck,
}

func runCheck(args []string) error {
	flags := flag.NewFlagSet("apranik check", flag.ContinueOnError)
	source := flags.String("config", defaultConfigSource, "file path or URL of the config file")
	fetchTimeout := flags.Duration("fetch-timeout", config.DefaultFetchTimeout, "how long to wait for a config file given as a URL")
	checkPorts := flags.Bool("ports", false, "also bind every inbound's address and release it")

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

	if err := builder.Verify(coreBlueprint, builder.VerifyOptions{CheckPorts: *checkPorts}); err != nil {
		return logFailure(logger, "Config cannot be built", "source", *source, "error", err)
	}

	logger.Info("Config is valid", "source", *source)
	return nil
}
