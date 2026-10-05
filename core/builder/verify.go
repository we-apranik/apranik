package builder

import (
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/spec"
)

// Routing is what every inbound shares: the outbounds, and the router that
// chooses between them.
type Routing struct {
	Outbounds []component.Outbound
	Router    component.Router
}

func (r Routing) Close() error {
	errs := make([]error, 0, len(r.Outbounds))
	for _, built := range r.Outbounds {
		errs = append(errs, built.Close())
	}
	return errors.Join(errs...)
}

// BuildRouting builds the datasets, the resolver, the outbounds and the router
// in the one order that works: the outbounds resolve through the resolver, and
// its servers go through the outbounds.
func (b *Builder) BuildRouting(coreBlueprint *blueprint.Blueprint) (Routing, error) {
	if len(coreBlueprint.Outbounds) == 0 {
		return Routing{}, errors.New("no outbounds configured")
	}

	datasets, err := b.LoadDatasets(coreBlueprint.Datasets)
	if err != nil {
		return Routing{}, err
	}

	if err := b.UseDNS(coreBlueprint.DNS); err != nil {
		return Routing{}, err
	}
	routing := Routing{Outbounds: make([]component.Outbound, 0, len(coreBlueprint.Outbounds))}
	for _, outboundBlueprint := range coreBlueprint.Outbounds {
		built, err := b.BuildOutbound(outboundBlueprint)
		if err != nil {
			routing.Close()
			return Routing{}, err
		}
		routing.Outbounds = append(routing.Outbounds, built)
	}
	if err := b.BuildDNS(coreBlueprint.DNS, routing.Outbounds, datasets); err != nil {
		routing.Close()
		return Routing{}, err
	}

	// One router for every inbound: it holds no per-connection state.
	if routing.Router, err = b.BuildRouter(coreBlueprint.Routing, routing.Outbounds, datasets); err != nil {
		routing.Close()
		return Routing{}, err
	}
	return routing, nil
}

// VerifyOptions says how far Verify goes beyond building.
type VerifyOptions struct {
	// Bind every inbound's address, as run would, and close the sockets again
	// at the end. Every inbound is bound before any is closed, so two
	// inbounds on one port are found too. Off by default: whether a port is
	// free is the machine's and the moment's, and a config is often checked
	// while the process it replaces still holds its ports.
	CheckPorts bool
}

// Verify builds everything run would and then discards it, so a refusal that
// only a constructor can make (a certificate that will not load, a dataset file
// that cannot be read) is found without serving. Nothing is dialed, and nothing
// is bound unless options.CheckPorts. The log outputs are not opened either: a
// log file is created on first use.
func Verify(coreBlueprint *blueprint.Blueprint, options VerifyOptions) error {
	stackBuilder, err := New(blueprint.Log{})
	if err != nil {
		return err
	}
	defer stackBuilder.Close()

	routing, err := stackBuilder.BuildRouting(coreBlueprint)
	if err != nil {
		return err
	}
	defer routing.Close()

	var refusals []error
	var heldListeners []spec.StreamListener
	defer func() {
		for _, listener := range heldListeners {
			listener.Close()
		}
	}()
	for _, inboundBlueprint := range coreBlueprint.Inbounds {
		if err := stackBuilder.CheckInbound(inboundBlueprint); err != nil {
			refusals = append(refusals, err)
			continue
		}
		if !options.CheckPorts {
			continue
		}
		listener, err := buildStreamListener(inboundBlueprint.StreamTransport, inboundBlueprint.Bind)
		if err != nil {
			refusals = append(refusals, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, err))
			continue
		}
		heldListeners = append(heldListeners, listener)
	}
	return errors.Join(refusals...)
}
