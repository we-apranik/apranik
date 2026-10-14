// Package apicoreprovider is what core gives every API module: one
// spec.APICoreProvider over the running core's statistics, logs and outbounds.
// It converts core's own types into the contract's in plain code, and turns
// core's own errors into the contract's, so core never learns an API's
// vocabulary and an API never imports core.
package apicoreprovider

import (
	"fmt"
	"log/slog"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

// SubscribeToLog starts a live view of the log; the builder's method and the
// logging system's Subscribe both have this shape.
type SubscribeToLog func(minimumLevel logging.Level, bufferedRecords int) (*logging.Subscription, error)

// RunningCore is the running core the provider speaks for: everything it reads
// and acts on, built before the provider and read-only after. The built
// inbounds and outbounds are here, not only their blueprints, because some
// calls act on them (switch a selector, change an inbound's users).
type RunningCore struct {
	Version string

	// Nil when the configuration counts nothing.
	Stats *stats.Registry

	Logs   SubscribeToLog
	Logger *logging.Logger

	// The configuration, for kinds, protocol names, the log outputs and the
	// default action.
	Blueprint *blueprint.Blueprint

	// The built inbounds, in the blueprint's order.
	Inbounds []component.Inbound

	// The built outbounds, in the blueprint's order.
	Outbounds []component.Outbound

	// Answers QueryDNS and FlushDNSCache. Nil when there is none.
	Resolver *dns.Resolver

	// Checks the configuration source and queues it to replace the running one;
	// see RequestReload. Nil when reloading is not offered.
	RequestReload func() error
}

type Provider struct {
	version   string
	stats     *stats.Registry
	logs      SubscribeToLog
	logger    *logging.Logger
	blueprint *blueprint.Blueprint

	requestReload func() error
	resolver      *dns.Resolver

	inboundsByTag  map[string]component.Inbound
	outboundTags   []string
	outboundsByTag map[string]component.Outbound
	blueprintByTag map[string]blueprint.Outbound
}

var _ spec.APICoreProvider = (*Provider)(nil)

func New(running RunningCore) (*Provider, error) {
	if running.Blueprint == nil {
		return nil, fmt.Errorf("apicoreprovider: a blueprint is needed")
	}
	if len(running.Inbounds) != len(running.Blueprint.Inbounds) {
		return nil, fmt.Errorf("apicoreprovider: %d inbounds were built for %d in the blueprint", len(running.Inbounds), len(running.Blueprint.Inbounds))
	}
	if len(running.Outbounds) != len(running.Blueprint.Outbounds) {
		return nil, fmt.Errorf("apicoreprovider: %d outbounds were built for %d in the blueprint", len(running.Outbounds), len(running.Blueprint.Outbounds))
	}
	logger := running.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	provider := &Provider{
		version:        running.Version,
		stats:          running.Stats,
		logs:           running.Logs,
		logger:         logger.With("component", "api"),
		blueprint:      running.Blueprint,
		requestReload:  running.RequestReload,
		resolver:       running.Resolver,
		inboundsByTag:  make(map[string]component.Inbound, len(running.Inbounds)),
		outboundTags:   make([]string, len(running.Outbounds)),
		outboundsByTag: make(map[string]component.Outbound, len(running.Outbounds)),
		blueprintByTag: make(map[string]blueprint.Outbound, len(running.Outbounds)),
	}
	for index, built := range running.Inbounds {
		provider.inboundsByTag[running.Blueprint.Inbounds[index].Tag] = built
	}
	for index, built := range running.Outbounds {
		tag := running.Blueprint.Outbounds[index].Tag
		provider.outboundTags[index] = tag
		provider.outboundsByTag[tag] = built
		provider.blueprintByTag[tag] = running.Blueprint.Outbounds[index]
	}
	return provider, nil
}

func (p *Provider) ApranikVersion() string { return p.version }

func (p *Provider) ConfiguredLogLevel() slog.Level { return p.blueprint.Log.Level }
