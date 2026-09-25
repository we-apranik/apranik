package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/spec"
)

const defaultConfigSource = "config.json"

var cmdRun = &Command{
	Name:  "run",
	Short: "load a config file and serve it",
	Usage: `Usage: apranik run [-config source] [-fetch-timeout duration]

Load the config file, check it, build the inbounds and outbounds
it describes, and serve until interrupted.

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

	report(diagnostics)
	if diagnostics.HasErrors() {
		return fmt.Errorf("%s has errors", *source)
	}

	return serve(coreBlueprint)
}

func serve(coreBlueprint *blueprint.Blueprint) error {
	stackBuilder, err := builder.New(coreBlueprint.Log)
	if err != nil {
		return err
	}
	defer stackBuilder.Close()
	logger := stackBuilder.Logger()

	if len(coreBlueprint.Outbounds) == 0 {
		return errors.New("no outbounds configured")
	}

	outbounds := make([]spec.Outbound, 0, len(coreBlueprint.Outbounds))
	for _, outboundBlueprint := range coreBlueprint.Outbounds {
		built, err := stackBuilder.BuildOutbound(outboundBlueprint)
		if err != nil {
			return err
		}
		defer built.Close()
		outbounds = append(outbounds, built)
	}

	// The router has no rules yet: every inbound uses the first outbound.
	defaultOutbound := outbounds[0]
	if len(outbounds) > 1 {
		logger.Warn("this build has no router, so only the first outbound is used",
			"using", defaultOutbound.Tag(), "configured", len(outbounds))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for _, inboundBlueprint := range coreBlueprint.Inbounds {
		built, err := stackBuilder.BuildInbound(inboundBlueprint)
		if err != nil {
			return err
		}
		defer built.Close()

		trafficHandler := handler.New(router.New(defaultOutbound), handler.Config{
			StreamIdleTimeout:     inboundBlueprint.TCPIdleTimeout,
			UDPSessionIdleTimeout: inboundBlueprint.UDPSessionIdleTimeout,
			Logger:                logger,
		})
		if err := built.Activate(ctx, trafficHandler); err != nil {
			return err
		}
		logger.Info("inbound active", "tag", inboundBlueprint.Tag, "network", inboundBlueprint.Network)
	}

	logger.Info("apranik is running", "version", blueprint.Version, "inbounds", len(coreBlueprint.Inbounds))
	<-ctx.Done()
	logger.Info("shutting down")
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

// Warnings first, so the last line is what stopped the run.
func report(diagnostics config.Diagnostics) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == config.SeverityWarning {
			fmt.Fprintln(os.Stderr, diagnostic)
		}
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == config.SeverityError {
			fmt.Fprintln(os.Stderr, diagnostic)
		}
	}
}
