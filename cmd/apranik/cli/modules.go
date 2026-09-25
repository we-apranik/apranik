package cli

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"text/tabwriter"

	"github.com/The-Apranik/Apranik/core/registry"
)

var cmdModules = &Command{
	Name:  "modules",
	Short: "list the modules this build can be configured with",
	Usage: `Usage: apranik modules

List every registered transport, proxy protocol and sniffer, with the kind it fills.
A "type" in a config file must name one of these.

Everything printed comes from the module itself, so a module describes itself
once and this listing never has to be updated.`,
	Run: runModules,
}

func runModules(args []string) error {
	flags := flag.NewFlagSet("apranik modules", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}

	modules, err := registry.Default()
	if err != nil {
		return fmt.Errorf("building the module registry: %w", err)
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer writer.Flush()

	fmt.Fprint(writer, "SUBSYSTEM\tNAME\tKIND\tDESCRIPTION\n")
	for _, listing := range modules.Transports().Modules() {
		fmt.Fprintf(writer, "transport\t%s\t%s\t%s\n", listing.Name, listing.Kind, listing.Description)
	}
	for _, listing := range modules.ProxyProtocols().Modules() {
		fmt.Fprintf(writer, "proxy\t%s\t%s\t%s\n", listing.Name, listing.Kind, listing.Description)
	}
	for _, listing := range modules.Sniffers().Modules() {
		fmt.Fprintf(writer, "sniffer\t%s\t%s\t%s\n", listing.Name, listing.Kind, listing.Description)
	}
	return nil
}

// Names only, deduplicated: one name can hold more than one kind.
func moduleNames(listings []registry.Listing) []string {
	var names []string
	for _, listing := range listings {
		if !slices.Contains(names, listing.Name) {
			names = append(names, listing.Name)
		}
	}
	return names
}
