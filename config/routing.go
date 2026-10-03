package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/route/match"
)

// The routing section and its datasets.
type rawRouting struct {
	Datasets      []rawDataset `json:"datasets"`
	Rules         []rawRule    `json:"rules"`
	DefaultAction *rawModule   `json:"default_action"`
}

type rawDataset struct {
	Tag    string           `json:"tag"`
	Source rawDatasetSource `json:"source"`
	Format rawDatasetFormat `json:"format"`
}

// Only a file for now; a URL is the expected next source.
type rawDatasetSource struct {
	File string `json:"file"`
}

// Flat, not {type, settings}: kind is needed by every format.
type rawDatasetFormat struct {
	Type      string `json:"type"`
	Kind      string `json:"kind"`
	Category  string `json:"category"`
	Attribute string `json:"attribute"`
}

type rawRule struct {
	Match  rawMatch   `json:"match"`
	Action *rawModule `json:"action"`
}

type rawMatch struct {
	Inbound         []string `json:"inbound"`
	Network         string   `json:"network"`
	User            []string `json:"user"`
	SniffedProtocol []string `json:"sniffed_protocol"`

	SniffedRequestHeader map[string]string `json:"sniffed_request_header"`
	SniffedBrowserFamily []string          `json:"sniffed_browser_family"`

	Domain        []string `json:"domain"`
	DomainSuffix  []string `json:"domain_suffix"`
	DomainKeyword []string `json:"domain_keyword"`
	DomainRegex   []string `json:"domain_regex"`

	IP          []string `json:"ip"`
	IPIsPrivate *bool    `json:"ip_is_private"`
	IPVersion   []int    `json:"ip_version"`
	Port        []uint16 `json:"port"`
	PortRange   []string `json:"port_range"`

	SourceIP          []string `json:"source_ip"`
	SourceIPIsPrivate *bool    `json:"source_ip_is_private"`
	SourcePort        []uint16 `json:"source_port"`
	SourcePortRange   []string `json:"source_port_range"`

	InboundLocalIP        []string `json:"inbound_local_ip"`
	InboundLocalPort      []uint16 `json:"inbound_local_port"`
	InboundLocalPortRange []string `json:"inbound_local_port_range"`

	DomainInDataset   []string `json:"domain_in_dataset"`
	IPInDataset       []string `json:"ip_in_dataset"`
	SourceIPInDataset []string `json:"source_ip_in_dataset"`

	Any []rawMatch `json:"any"`
	All []rawMatch `json:"all"`
	Not *rawMatch  `json:"not"`
}

type rawRouteSettings struct {
	Outbound string `json:"outbound"`
}

type rawSniffSettings struct {
	Sniffers []rawModule    `json:"sniffers"`
	Timeout  types.Duration `json:"timeout"`
}

// Action types, as the config file names them.
const (
	actionRoute = "route"
	actionBlock = "block"
	actionSniff = "sniff"
)

func (b *configBuilder) buildRouting(raw rawRouting, loc string) *blueprint.Routing {
	routing := &blueprint.Routing{}

	for index, dataset := range raw.Datasets {
		routing.Datasets = append(routing.Datasets,
			b.buildDataset(dataset, indexLocation(fieldLocation(loc, "datasets"), index)))
	}
	for index, rule := range raw.Rules {
		ruleLoc := indexLocation(fieldLocation(loc, "rules"), index)
		built := blueprint.Rule{Match: b.buildMatch(rule.Match, fieldLocation(ruleLoc, "match"))}
		if rule.Action == nil {
			b.diagnostics.report(fieldLocation(ruleLoc, "action"), errMissingField("action", "rule"))
		} else {
			built.Action = b.buildAction(*rule.Action, fieldLocation(ruleLoc, "action"))
		}
		routing.Rules = append(routing.Rules, built)
	}

	defaultLoc := fieldLocation(loc, "default_action")
	if raw.DefaultAction == nil {
		b.diagnostics.errorf(defaultLoc, `routing needs a default_action, such as {"type": "route", "settings": {"outbound": "proxy"}}`)
	} else {
		routing.DefaultAction = b.buildAction(*raw.DefaultAction, defaultLoc)
	}
	return routing
}

func (b *configBuilder) buildDataset(raw rawDataset, loc string) blueprint.Dataset {
	dataset := blueprint.Dataset{
		Tag:       raw.Tag,
		Kind:      blueprint.DatasetKind(raw.Format.Kind),
		Format:    raw.Format.Type,
		Category:  raw.Format.Category,
		Attribute: raw.Format.Attribute,
		File:      raw.Source.File,
	}
	if dataset.File != "" && !filepath.IsAbs(dataset.File) && b.baseDirectory != "" {
		dataset.File = filepath.Join(b.baseDirectory, dataset.File)
	}
	return dataset
}

func (b *configBuilder) buildMatch(raw rawMatch, loc string) blueprint.Match {
	built := blueprint.Match{
		Inbounds:               raw.Inbound,
		Users:                  raw.User,
		SniffedProtocols:       raw.SniffedProtocol,
		SniffedBrowserFamilies: lowercase(raw.SniffedBrowserFamily),
		DomainKeywords:         lowercase(raw.DomainKeyword),
		IPIsPrivate:            raw.IPIsPrivate,
		SourceIPIsPrivate:      raw.SourceIPIsPrivate,
		DomainDatasets:         raw.DomainInDataset,
		IPDatasets:             raw.IPInDataset,
		SourceIPDatasets:       raw.SourceIPInDataset,
	}

	if raw.Network != "" {
		built.Network = b.parseNetwork(raw.Network, fieldLocation(loc, "network"))
	}

	built.Domains = b.normalizeDomains(raw.Domain, fieldLocation(loc, "domain"), false)
	built.DomainSuffixes = b.normalizeDomains(raw.DomainSuffix, fieldLocation(loc, "domain_suffix"), true)
	for index, pattern := range raw.DomainRegex {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			b.diagnostics.report(indexLocation(fieldLocation(loc, "domain_regex"), index), err)
			continue
		}
		built.DomainRegexes = append(built.DomainRegexes, regex)
	}

	built.IPs = b.parseAddressesOrRanges(raw.IP, fieldLocation(loc, "ip"))
	built.SourceIPs = b.parseAddressesOrRanges(raw.SourceIP, fieldLocation(loc, "source_ip"))
	built.InboundLocalIPs = b.parseAddressesOrRanges(raw.InboundLocalIP, fieldLocation(loc, "inbound_local_ip"))

	for index, version := range raw.IPVersion {
		if version != 4 && version != 6 {
			b.diagnostics.errorf(indexLocation(fieldLocation(loc, "ip_version"), index), "ip_version %d is neither 4 nor 6", version)
			continue
		}
		built.IPVersions = append(built.IPVersions, version)
	}

	built.Ports = b.parsePortsAndRanges(raw.Port, raw.PortRange, fieldLocation(loc, "port_range"))
	built.SourcePorts = b.parsePortsAndRanges(raw.SourcePort, raw.SourcePortRange, fieldLocation(loc, "source_port_range"))
	built.InboundLocalPorts = b.parsePortsAndRanges(raw.InboundLocalPort, raw.InboundLocalPortRange, fieldLocation(loc, "inbound_local_port_range"))

	if len(raw.SniffedRequestHeader) > 0 {
		built.SniffedRequestHeaders = make(map[string]*regexp.Regexp, len(raw.SniffedRequestHeader))
		headersLoc := fieldLocation(loc, "sniffed_request_header")
		for _, name := range slices.Sorted(maps.Keys(raw.SniffedRequestHeader)) {
			pattern := raw.SniffedRequestHeader[name]
			regex, err := regexp.Compile(pattern)
			if err != nil {
				b.diagnostics.report(fieldLocation(headersLoc, name), err)
				continue
			}
			built.SniffedRequestHeaders[strings.ToLower(name)] = regex
		}
	}

	for index, alternative := range raw.Any {
		built.Any = append(built.Any, b.buildMatch(alternative, indexLocation(fieldLocation(loc, "any"), index)))
	}
	for index, required := range raw.All {
		built.All = append(built.All, b.buildMatch(required, indexLocation(fieldLocation(loc, "all"), index)))
	}
	if raw.Not != nil {
		negated := b.buildMatch(*raw.Not, fieldLocation(loc, "not"))
		built.Not = &negated
	}
	return built
}

func (b *configBuilder) normalizeDomains(names []string, loc string, isSuffix bool) []string {
	var normalized []string
	for index, name := range names {
		if isSuffix {
			name = strings.TrimLeft(name, ".")
		}
		name = match.NormalizeDomain(strings.TrimSpace(name))
		if name == "" {
			b.diagnostics.errorf(indexLocation(loc, index), "an empty domain matches nothing")
			continue
		}
		normalized = append(normalized, name)
	}
	return normalized
}

func lowercase(values []string) []string {
	var lowered []string
	for _, value := range values {
		lowered = append(lowered, strings.ToLower(value))
	}
	return lowered
}

// An address or a CIDR: "1.2.3.4" is 1.2.3.4/32.
func (b *configBuilder) parseAddressesOrRanges(values []string, loc string) []netip.Prefix {
	var prefixes []netip.Prefix
	for index, value := range values {
		if address, err := netip.ParseAddr(value); err == nil {
			address = address.Unmap()
			prefixes = append(prefixes, netip.PrefixFrom(address, address.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			b.diagnostics.errorf(indexLocation(loc, index), "%q is neither an IP address nor a CIDR range", value)
			continue
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}

// Single ports and "first-last" ranges, merged into one list.
func (b *configBuilder) parsePortsAndRanges(ports []uint16, rangeTexts []string, rangesLoc string) []blueprint.PortRange {
	var portRanges []blueprint.PortRange
	for _, port := range ports {
		portRanges = append(portRanges, blueprint.PortRange{First: port, Last: port})
	}
	for index, text := range rangeTexts {
		portRange, err := parsePortRange(text)
		if err != nil {
			b.diagnostics.report(indexLocation(rangesLoc, index), err)
			continue
		}
		portRanges = append(portRanges, portRange)
	}
	return portRanges
}

// "1000-2000", inclusive.
func parsePortRange(text string) (blueprint.PortRange, error) {
	firstText, lastText, found := strings.Cut(text, "-")
	if !found {
		return blueprint.PortRange{}, fmt.Errorf("%q is not a range; write \"first-last\", or list a single port in the field without _range", text)
	}
	first, firstErr := strconv.ParseUint(strings.TrimSpace(firstText), 10, 16)
	last, lastErr := strconv.ParseUint(strings.TrimSpace(lastText), 10, 16)
	if firstErr != nil || lastErr != nil {
		return blueprint.PortRange{}, fmt.Errorf("%q is not a range of ports 0-65535", text)
	}
	if first > last {
		return blueprint.PortRange{}, fmt.Errorf("%q starts after it ends", text)
	}
	return blueprint.PortRange{First: uint16(first), Last: uint16(last)}, nil
}

func (b *configBuilder) buildAction(raw rawModule, loc string) blueprint.Action {
	settingsLoc := fieldLocation(loc, "settings")
	switch raw.Type {
	case "":
		b.diagnostics.report(loc, errMissingField("type", "action"))
	case actionRoute:
		var settings rawRouteSettings
		if !b.decodeStrict(raw.Settings, &settings, settingsLoc) {
			break
		}
		if settings.Outbound == "" {
			b.diagnostics.report(settingsLoc, errMissingField("outbound", "route action"))
			break
		}
		return blueprint.Action{Route: &blueprint.RouteAction{Outbound: settings.Outbound}}
	case actionBlock:
		if hasSettings(raw.Settings) {
			b.diagnostics.errorf(settingsLoc, "block takes no settings")
		}
		return blueprint.Action{Block: true}
	case actionSniff:
		var settings rawSniffSettings
		if !b.decodeStrict(raw.Settings, &settings, settingsLoc) {
			break
		}
		return blueprint.Action{Sniff: b.buildSniffAction(settings, settingsLoc)}
	default:
		b.diagnostics.errorf(fieldLocation(loc, "type"), "unknown action %q; want %q, %q or %q", raw.Type, actionRoute, actionBlock, actionSniff)
	}
	return blueprint.Action{}
}

func (b *configBuilder) buildSniffAction(settings rawSniffSettings, loc string) *blueprint.SniffAction {
	sniff := &blueprint.SniffAction{Timeout: time.Duration(settings.Timeout)}
	for index, rawSniffer := range settings.Sniffers {
		snifferLoc := indexLocation(fieldLocation(loc, "sniffers"), index)
		newModule, err := b.modules.Sniffers().Lookup(rawSniffer.Type)
		if err != nil {
			b.diagnostics.report(snifferLoc, err)
			continue
		}
		sniffer := newModule()
		if b.decodeSettings(sniffer, rawSniffer.Settings, fieldLocation(snifferLoc, "settings")) {
			sniff.Sniffers = append(sniff.Sniffers, sniffer)
		}
	}
	return sniff
}

// Like decodeSettings, for core's own settings types, which have no Validate.
func (b *configBuilder) decodeStrict(raw json.RawMessage, target any, loc string) bool {
	if len(raw) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if b.strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		b.diagnostics.report(loc, err)
		return false
	}
	return true
}
