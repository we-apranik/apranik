package config

import (
	"net/url"
	"slices"
	"strings"

	"github.com/The-Apranik/Apranik/core/blueprint"
)

// A group reaches no server and has no transport of its own: its members do.
func checkGroupOutboundFields(diagnostics *Diagnostics, outbound blueprint.Outbound, loc string) {
	groupType := outbound.Group.Type
	if outbound.Server.IsValid() {
		diagnostics.errorf(fieldLocation(loc, "server"), "%s reaches no proxy server, so it takes no server", groupType)
	}
	if outbound.DNSServer != "" {
		diagnostics.errorf(fieldLocation(loc, "dns_server"),
			"%s has no server name of its own to resolve; set dns_server on a member", groupType)
	}
	if !outbound.StreamTransport.IsZero() {
		diagnostics.errorf(fieldLocation(loc, "transport"), "%s has no transport of its own; its members have theirs", groupType)
	}
}

// checkGroups checks what only the whole list of outbounds can answer: whether
// each member exists, whether groups wait on each other, and each group's probe.
func checkGroups(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	outbounds := make(map[string]blueprint.Outbound, len(coreBlueprint.Outbounds))
	for _, outbound := range coreBlueprint.Outbounds {
		outbounds[outbound.Tag] = outbound
	}

	for index, outbound := range coreBlueprint.Outbounds {
		if outbound.Group == nil {
			continue
		}
		loc := fieldLocation(indexLocation("outbounds", index), "protocol", "settings")
		checkGroupMembers(diagnostics, outbound, outbounds, fieldLocation(loc, "outbounds"))
		checkGroupProbe(diagnostics, *outbound.Group, fieldLocation(loc, "probe"))
		checkGroupTolerance(diagnostics, *outbound.Group, fieldLocation(loc, "tolerance"))
	}
	checkGroupLoops(diagnostics, coreBlueprint, outbounds)
}

func checkGroupMembers(diagnostics *Diagnostics, outbound blueprint.Outbound, outbounds map[string]blueprint.Outbound, loc string) {
	members := outbound.Group.Members
	switch len(members) {
	case 0:
		diagnostics.errorf(loc, "a %s group needs members: the tags of the outbounds it picks among", outbound.Group.Type)
		return
	case 1:
		diagnostics.warnf(loc, "a group with one member has nothing to pick between")
	}

	seen := make(map[string]int, len(members))
	for position, tag := range members {
		memberLoc := indexLocation(loc, position)
		switch first, repeated := seen[tag]; {
		case repeated:
			diagnostics.warnf(memberLoc, "%q is already a member at index %d", tag, first)
			continue
		case tag == outbound.Tag:
			diagnostics.errorf(memberLoc, "a group cannot be its own member")
		case !hasOutbound(outbounds, tag):
			diagnostics.errorf(memberLoc, "no outbound is tagged %q", tag)
		}
		seen[tag] = position
	}
}

func hasOutbound(outbounds map[string]blueprint.Outbound, tag string) bool {
	_, found := outbounds[tag]
	return found
}

func checkGroupProbe(diagnostics *Diagnostics, group blueprint.Group, loc string) {
	probe := group.Probe
	switch probe.Type {
	case "":
		diagnostics.errorf(fieldLocation(loc, "type"), `a %s group needs a probe: {"type": "http"} or {"type": "https"}`, group.Type)
		return
	case blueprint.ProbeTypeHTTP, blueprint.ProbeTypeHTTPS:
	default:
		diagnostics.errorf(fieldLocation(loc, "type"), "unknown probe type %q; want %s or %s", probe.Type, blueprint.ProbeTypeHTTP, blueprint.ProbeTypeHTTPS)
		return
	}

	if probe.URL != "" {
		parsed, err := url.Parse(probe.URL)
		switch {
		case err != nil:
			diagnostics.report(fieldLocation(loc, "url"), err)
		case parsed.Scheme != string(probe.Type):
			diagnostics.errorf(fieldLocation(loc, "url"), "a %s probe needs a url starting with %s://, got %q", probe.Type, probe.Type, probe.URL)
		case parsed.Hostname() == "":
			diagnostics.errorf(fieldLocation(loc, "url"), "url %q names no host", probe.URL)
		}
	}
	if probe.Interval < 0 {
		diagnostics.errorf(fieldLocation(loc, "interval"), "interval must not be negative")
	}
	if probe.Timeout < 0 {
		diagnostics.errorf(fieldLocation(loc, "timeout"), "timeout must not be negative")
	}
}

func checkGroupTolerance(diagnostics *Diagnostics, group blueprint.Group, loc string) {
	switch {
	case group.Tolerance < 0:
		diagnostics.errorf(loc, "tolerance must not be negative")
	case group.Tolerance != 0 && group.Type != blueprint.GroupTypeURLTest:
		diagnostics.errorf(loc, "tolerance is how much faster another member must be before %s switches to it; %s never compares speed, so it takes none",
			blueprint.GroupTypeURLTest, group.Type)
	}
}

// Groups waiting on each other would never finish building.
func checkGroupLoops(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint, outbounds map[string]blueprint.Outbound) {
	const (
		unvisited = iota
		onPath
		finished
	)
	index := make(map[string]int, len(coreBlueprint.Outbounds))
	for position, outbound := range coreBlueprint.Outbounds {
		index[outbound.Tag] = position
	}
	state := map[string]int{}
	var path []string
	reported := map[string]bool{}

	var visit func(tag string)
	visit = func(tag string) {
		state[tag] = onPath
		path = append(path, tag)
		for _, member := range outbounds[tag].Group.Members {
			if memberOutbound, found := outbounds[member]; !found || memberOutbound.Group == nil || member == tag {
				continue
			}
			switch state[member] {
			case onPath:
				loop := path[slices.Index(path, member):]
				key := loopKey(loop)
				if reported[key] {
					continue
				}
				reported[key] = true
				names := make([]string, 0, len(loop)+1)
				for _, step := range loop {
					names = append(names, `"`+step+`"`)
				}
				names = append(names, `"`+member+`"`)
				diagnostics.errorf(indexLocation("outbounds", index[member]), "these groups pick among each other: %s", strings.Join(names, " → "))
			case unvisited:
				visit(member)
			}
		}
		path = path[:len(path)-1]
		state[tag] = finished
	}
	for _, outbound := range coreBlueprint.Outbounds {
		if outbound.Group != nil && state[outbound.Tag] == unvisited {
			visit(outbound.Tag)
		}
	}
}
