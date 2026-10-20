package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/spec"
)

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

func (b *Builder) BuildRouting(coreBlueprint *blueprint.Blueprint) (Routing, error) {
	if len(coreBlueprint.Outbounds) == 0 {
		return Routing{}, errors.New("no outbounds configured")
	}

	b.BuildStats(coreBlueprint)

	datasets, err := b.LoadDatasets(coreBlueprint.Datasets)
	if err != nil {
		return Routing{}, err
	}

	if err := b.UseDNS(coreBlueprint.DNS); err != nil {
		return Routing{}, err
	}
	outbounds, err := b.BuildOutbounds(coreBlueprint.Outbounds)
	if err != nil {
		return Routing{}, err
	}
	routing := Routing{Outbounds: outbounds}
	if err := b.BuildDNS(coreBlueprint.DNS, routing.Outbounds, datasets); err != nil {
		routing.Close()
		return Routing{}, err
	}

	if routing.Router, err = b.BuildRouter(coreBlueprint.Routing, routing.Outbounds, datasets); err != nil {
		routing.Close()
		return Routing{}, err
	}
	return routing, nil
}

// StartProbing starts every outbound that tests its members in the
// background. Run calls it once; Verify never does, so checking a config dials
// nothing.
func (r Routing) StartProbing(ctx context.Context) {
	for _, built := range r.Outbounds {
		if probing, ok := built.(component.ProbingOutbound); ok {
			probing.StartProbing(ctx)
		}
	}
}

type VerifyOptions struct {
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
