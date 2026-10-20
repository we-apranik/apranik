package cli

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/we-apranik/apranik/core/registry"
)

var cmdModules = &Command{
	Name:  "modules",
	Short: "list the modules this build can be configured with",
	Usage: `Usage: apranik modules

List every registered transport, proxy protocol, sniffer, DNS server and API,
grouped by the kind it fills; a module filling several kinds appears under each.
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

	// Padded by hand, not with tabwriter
	groups := modulesByKind(modules)
	nameWidth := 0
	for _, group := range groups {
		for _, listing := range group.listings {
			nameWidth = max(nameWidth, len(listing.Name))
		}
	}
	for index, group := range groups {
		if index > 0 {
			fmt.Println()
		}
		fmt.Println(group.heading())
		for _, listing := range group.listings {
			fmt.Printf("  %-*s  %s\n", nameWidth, listing.Name, listing.Description)
		}
	}
	return nil
}

type kindGroup struct {
	kind     registry.Kind
	listings []registry.Listing
}

func (g kindGroup) heading() string {
	name := g.kind.String()
	if plural, isProxy := strings.CutSuffix(name, "proxy"); isProxy {
		name = plural + "proxies"
	} else {
		name += "s"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func modulesByKind(modules *registry.Registry) []kindGroup {
	var all []registry.Listing
	all = append(all, modules.Transports().Modules()...)
	all = append(all, modules.ProxyProtocols().Modules()...)
	all = append(all, modules.Sniffers().Modules()...)
	all = append(all, modules.DNSServers().Modules()...)
	all = append(all, modules.APIs().Modules()...)
	slices.SortStableFunc(all, func(first, second registry.Listing) int {
		return int(first.Kind) - int(second.Kind)
	})

	var groups []kindGroup
	for _, listing := range all {
		if len(groups) == 0 || groups[len(groups)-1].kind != listing.Kind {
			groups = append(groups, kindGroup{kind: listing.Kind})
		}
		last := &groups[len(groups)-1]
		last.listings = append(last.listings, listing)
	}
	return groups
}

func (g kindGroup) names() []string {
	names := make([]string, len(g.listings))
	for index, listing := range g.listings {
		names[index] = listing.Name
	}
	return names
}
