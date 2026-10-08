// Package apicoreprovider is what core gives every API module: one
// spec.APICoreProvider over the running core's statistics, logs and outbounds.
// It converts core's own types into the contract's in plain code, and turns
// core's own errors into the contract's, so core never learns an API's
// vocabulary and an API never imports core.
package apicoreprovider

import (
	"fmt"
	"log/slog"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/dns"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

// SubscribeToLog starts a live view of the log; the builder's method and the
// logging system's Subscribe both have this shape.
type SubscribeToLog func(minimumLevel logging.Level, bufferedRecords int) (*logging.Subscription, error)

// Parts is what the provider reads. Everything is built before the provider
// and read-only after.
type Parts struct {
	Version string

	// Nil when the configuration counts nothing.
	Stats *stats.Registry

	Logs   SubscribeToLog
	Logger *logging.Logger

	// The configuration, for kinds, protocol names, the log outputs and the
	// default action.
	Blueprint *blueprint.Blueprint

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

	outboundTags   []string
	outboundsByTag map[string]component.Outbound
	blueprintByTag map[string]blueprint.Outbound
}

var _ spec.APICoreProvider = (*Provider)(nil)

func New(parts Parts) (*Provider, error) {
	if parts.Blueprint == nil {
		return nil, fmt.Errorf("apicoreprovider: a blueprint is needed")
	}
	if len(parts.Outbounds) != len(parts.Blueprint.Outbounds) {
		return nil, fmt.Errorf("apicoreprovider: %d outbounds were built for %d in the blueprint", len(parts.Outbounds), len(parts.Blueprint.Outbounds))
	}
	logger := parts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	provider := &Provider{
		version:        parts.Version,
		stats:          parts.Stats,
		logs:           parts.Logs,
		logger:         logger.With("component", "api"),
		blueprint:      parts.Blueprint,
		requestReload:  parts.RequestReload,
		resolver:       parts.Resolver,
		outboundTags:   make([]string, len(parts.Outbounds)),
		outboundsByTag: make(map[string]component.Outbound, len(parts.Outbounds)),
		blueprintByTag: make(map[string]blueprint.Outbound, len(parts.Outbounds)),
	}
	for index, built := range parts.Outbounds {
		tag := parts.Blueprint.Outbounds[index].Tag
		provider.outboundTags[index] = tag
		provider.outboundsByTag[tag] = built
		provider.blueprintByTag[tag] = parts.Blueprint.Outbounds[index]
	}
	return provider, nil
}

func (p *Provider) ApranikVersion() string { return p.version }

func (p *Provider) ConfiguredLogLevel() slog.Level { return p.blueprint.Log.Level }
