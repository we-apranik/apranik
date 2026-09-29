package cli

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	"github.com/The-Apranik/Apranik/core/registry"
)

// Overridable with -ldflags "-X github.com/The-Apranik/Apranik/cmd/cli.version=1.2.3".
var version = "dev"

var cmdVersion = &Command{
	Name:  "version",
	Short: "print version and build information",
	Usage: `Usage: apranik version

Print the version this binary was built as, the Go toolchain that built it,
the target platform, the commit it was built from, and the modules compiled
into it.`,
	Run: runVersion,
}

func runVersion(args []string) error {
	flags := flag.NewFlagSet("apranik version", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}

	fmt.Printf("apranik %s\n", version)

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer writer.Flush()
	fmt.Fprintf(writer, "  go\t%s\n", runtime.Version())
	fmt.Fprintf(writer, "  platform\t%s/%s\n", runtime.GOOS, runtime.GOARCH)
	if revision, ok := buildRevision(); ok {
		fmt.Fprintf(writer, "  revision\t%s\n", revision)
	}

	// Two builds at the same version can accept different configurations.
	modules, err := registry.Default()
	if err != nil {
		return fmt.Errorf("building the module registry: %w", err)
	}
	fmt.Fprintln(writer)
	for _, group := range modulesByKind(modules) {
		fmt.Fprintf(writer, "  %s\t%s\n", group.heading(), strings.Join(group.names(), ", "))
	}
	return nil
}

// Absent unless the build recorded a VCS stamp, which go run does not.
func buildRevision() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return setting.Value, true
		}
	}
	return "", false
}
