package cli

import (
	"flag"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

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
	fmt.Printf("  go         %s\n", runtime.Version())
	fmt.Printf("  platform   %s/%s\n", runtime.GOOS, runtime.GOARCH)

	if revision, ok := buildRevision(); ok {
		fmt.Printf("  revision   %s\n", revision)
	}

	// Two builds at the same version can accept different configurations.
	modules, err := registry.Default()
	if err != nil {
		return fmt.Errorf("building the module registry: %w", err)
	}
	fmt.Printf("  transports %s\n", strings.Join(moduleNames(modules.Transports().Modules()), ", "))
	fmt.Printf("  proxies    %s\n", strings.Join(moduleNames(modules.ProxyProtocols().Modules()), ", "))
	fmt.Printf("  sniffers   %s\n", strings.Join(moduleNames(modules.Sniffers().Modules()), ", "))
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
