// Command apranik runs Apranik as a standalone process, so the library can be
// used and tested without an embedding app. It loads a configuration
// config file and serves it, or checks one without running it.
//
//	apranik run [-config path]     load a config file and serve it
//	apranik check [-config path]   check a config file without running it
//	apranik modules                list the modules compiled in
//	apranik generate uuid          generate a value a config file needs
//	apranik version                version and build information
//	apranik help [command]         usage
//
// main owns the control flow and is the only place that calls os.Exit.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/we-apranik/apranik/cmd/apranik/cli"
)

const (
	exitSuccess = 0 // also help
	exitFailure = 1
	exitUsage   = 2 // no such command
)

func main() {
	name, args := parseInvocation(os.Args[1:])

	command := cli.Lookup(name)
	if command == nil {
		fmt.Fprintf(os.Stderr, "apranik: unknown command %q\nRun \"apranik help\" for usage.\n", name)
		os.Exit(exitUsage)
	}

	if err := command.Run(args); err != nil {
		// -h on a subcommand: flag printed the usage already.
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(exitSuccess)
		}
		if errors.Is(err, cli.ErrReported) {
			os.Exit(exitFailure)
		}
		fmt.Fprintln(os.Stderr, "apranik:", err)
		os.Exit(exitFailure)
	}

	os.Exit(exitSuccess)
}

// A bare invocation and the help flags all mean help, so everything
// downstream sees a command name.
func parseInvocation(argv []string) (name string, args []string) {
	if len(argv) == 0 {
		return "help", nil
	}

	switch argv[0] {
	case "-h", "-help", "--help":
		return "help", nil
	default:
		return argv[0], argv[1:]
	}
}
