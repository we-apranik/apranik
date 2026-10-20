package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/we-apranik/apranik/core/apicoreprovider"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/builder"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/route/handler"
	"github.com/we-apranik/apranik/spec"
)

func serve(source string, fetchTimeout time.Duration, coreBlueprint *blueprint.Blueprint) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hangups := make(chan os.Signal, 1)
	signal.Notify(hangups, syscall.SIGHUP)
	defer signal.Stop(hangups)

	return serveReloadable(ctx, coreBlueprint, newReloader(source, fetchTimeout), hangups, startCore)
}

type coreStarter func(ctx context.Context, coreBlueprint *blueprint.Blueprint, requestReload func() error) (*runningCore, error)

func serveReloadable(ctx context.Context, first *blueprint.Blueprint, reloads *reloader, hangups <-chan os.Signal, start coreStarter) error {
	current := first
	running, err := start(ctx, current, reloads.Request)
	if err != nil {
		return err
	}
	reloads.logger.Store(running.logger)

	for {
		select {
		case <-ctx.Done():
			running.logger.Info("Shutting down")
			running.Close()
			return nil

		case <-hangups:
			if err := reloads.Request(); err != nil {
				running.logger.Error("Reload refused, the running configuration is kept", "error", err)
			}

		case next := <-reloads.pending:
			running.logger.Info("Reloading the configuration", "source", reloads.source)
			running.Close()
			started, err := start(ctx, next, reloads.Request)
			if err != nil {
				newBootstrapLogger(next).Error("Reloaded configuration did not start, starting the previous one again", "error", err)
				started, err = start(ctx, current, reloads.Request)
				if err != nil {
					return fmt.Errorf("starting the previous configuration again: %w", err)
				}
			} else {
				current = next
			}
			running = started
			reloads.logger.Store(running.logger)
		}
	}
}

type reloader struct {
	source       string
	fetchTimeout time.Duration
	pending      chan *blueprint.Blueprint
	logger       atomic.Pointer[logging.Logger]
}

func newReloader(source string, fetchTimeout time.Duration) *reloader {
	return &reloader{source: source, fetchTimeout: fetchTimeout, pending: make(chan *blueprint.Blueprint, 1)}
}

func (r *reloader) Request() error {
	next, err := r.checkedConfiguration()
	if err != nil {
		return err
	}
	select {
	case r.pending <- next:
		return nil
	default:
		return spec.ErrAPIReloadInProgress
	}
}

func (r *reloader) checkedConfiguration() (*blueprint.Blueprint, error) {
	next, diagnostics, err := load(r.source, r.fetchTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", spec.ErrAPIInvalidConfiguration, err)
	}
	logger := r.logger.Load()
	if logger == nil {
		logger = logging.Discard()
	}
	report(logger, diagnostics)
	if diagnostics.HasErrors() {
		return nil, fmt.Errorf("%w: %w", spec.ErrAPIInvalidConfiguration, diagnostics.Err())
	}
	if err := builder.Verify(next, builder.VerifyOptions{}); err != nil {
		return nil, fmt.Errorf("%w: %w", spec.ErrAPIInvalidConfiguration, err)
	}
	return next, nil
}

type runningCore struct {
	logger  *logging.Logger
	closers []func()
}

func (c *runningCore) closeLast(closer func()) { c.closers = append(c.closers, closer) }

func (c *runningCore) Close() {
	for index := len(c.closers) - 1; index >= 0; index-- {
		c.closers[index]()
	}
	c.closers = nil
}

func startCore(ctx context.Context, coreBlueprint *blueprint.Blueprint, requestReload func() error) (*runningCore, error) {
	stackBuilder, err := builder.New(coreBlueprint.Log)
	if err != nil {
		return nil, err
	}
	core := &runningCore{logger: stackBuilder.Logger()}
	core.closeLast(func() { stackBuilder.Close() })
	logger := core.logger

	if err := startComponents(ctx, core, stackBuilder, coreBlueprint, requestReload); err != nil {
		core.Close()
		return nil, err
	}
	logger.Info("Apranik is running", "version", blueprint.Version, "inbounds", len(coreBlueprint.Inbounds), "apis", len(coreBlueprint.APIs))
	return core, nil
}

func startComponents(ctx context.Context, core *runningCore, stackBuilder *builder.Builder, coreBlueprint *blueprint.Blueprint, requestReload func() error) error {
	logger := core.logger
	routing, err := stackBuilder.BuildRouting(coreBlueprint)
	if err != nil {
		return err
	}
	core.closeLast(func() { routing.Close() })

	coreContext, stopCore := context.WithCancel(ctx)
	core.closeLast(stopCore)
	routing.StartProbing(coreContext)

	var tracking blueprint.StatsTracking
	if coreBlueprint.Stats != nil {
		tracking = coreBlueprint.Stats.Tracking
	}
	builtInbounds := make([]component.Inbound, 0, len(coreBlueprint.Inbounds))
	for _, inboundBlueprint := range coreBlueprint.Inbounds {
		built, err := stackBuilder.BuildInbound(inboundBlueprint)
		if err != nil {
			return err
		}
		builtInbounds = append(builtInbounds, built)
		core.closeLast(func() { built.Close() })

		trafficHandler := handler.New(routing.Router, handler.Config{
			StreamIdleTimeout:                   valueOrZero(inboundBlueprint.TCPIdleTimeout),
			UDPSessionIdleTimeout:               valueOrZero(inboundBlueprint.UDPSessionIdleTimeout),
			UDPMaxDatagramSize:                  valueOrZero(inboundBlueprint.UDPMaxDatagramSize),
			UDPRoutingCacheEntryIdleTimeout:     valueOrZero(inboundBlueprint.UDPRoutingCacheEntryIdleTimeout),
			UDPRoutingCacheMaxEntriesPerSession: valueOrZero(inboundBlueprint.UDPRoutingCacheMaxEntriesPerSession),
			ResumeUDPSessions:                   inboundBlueprint.MuxCool.XUDPGlobalIDEnabled(),
			UDPSessionResumeHoldTime:            inboundBlueprint.MuxCool.XUDPGlobalIDHoldTime(),

			LiveTrackingUDPEntryIdleTimeout:     valueOrZero(tracking.UDPEntryIdleTimeout),
			LiveTrackingUDPMaxEntriesPerSession: valueOrZero(tracking.UDPMaxEntriesPerSession),
			Resolver:                            stackBuilder.Resolver(),
			Stats:                               stackBuilder.Stats(),
			InboundProtocolName:                 builder.InboundProtocolName(inboundBlueprint),
			Logger:                              logger,
		})
		if err := built.Activate(coreContext, trafficHandler); err != nil {
			return err
		}
		logger.Info("Inbound active", "tag", inboundBlueprint.Tag, "network", inboundBlueprint.Network)
	}

	if len(coreBlueprint.APIs) > 0 {
		coreProvider, err := apicoreprovider.New(apicoreprovider.RunningCore{
			Version:       blueprint.Version,
			Stats:         stackBuilder.Stats(),
			Logs:          stackBuilder.SubscribeToLog,
			Logger:        logger,
			Blueprint:     coreBlueprint,
			Inbounds:      builtInbounds,
			Outbounds:     routing.Outbounds,
			Resolver:      stackBuilder.Resolver(),
			RequestReload: requestReload,
		})
		if err != nil {
			return err
		}
		servers, err := startAPIs(coreContext, coreBlueprint.APIs, coreProvider, logger)
		if err != nil {
			return err
		}
		core.closeLast(func() { closeAPIs(servers, logger) })
	}
	return nil
}
