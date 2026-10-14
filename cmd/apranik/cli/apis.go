package cli

import (
	"context"
	"fmt"

	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

// startAPIs builds and starts each API in order, last of everything, so what
// they read is running. A server that cannot start, a taken port for one, stops
// the ones already started and ends run with its error, as an inbound that
// cannot bind does: nobody gets a core that is half up.
func startAPIs(ctx context.Context, apis []spec.APIModule, coreProvider spec.APICoreProvider, logger *logging.Logger) ([]spec.APIServer, error) {
	var started []spec.APIServer
	for index, module := range apis {
		server, err := module.NewAPIServer()
		if err != nil {
			closeAPIs(started, logger)
			return nil, fmt.Errorf("apis[%d] (%s): %w", index, module.Info().Name, err)
		}
		if err := server.Start(ctx, coreProvider); err != nil {
			closeAPIs(started, logger)
			return nil, fmt.Errorf("apis[%d] (%s): start: %w", index, module.Info().Name, err)
		}
		started = append(started, server)
		logger.Info("API started", "index", index, "type", module.Info().Name)
	}
	return started, nil
}

// closeAPIs closes in the reverse of the order they started, and each one ends
// its open requests before it returns.
func closeAPIs(servers []spec.APIServer, logger *logging.Logger) {
	for index := len(servers) - 1; index >= 0; index-- {
		if err := servers[index].Close(); err != nil {
			logger.Warn("API did not close cleanly", "index", index, "error", err)
		}
	}
}
