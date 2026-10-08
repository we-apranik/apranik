package apicoreprovider

import "errors"

var (
	// The configuration source could not be read, has errors, or cannot be
	// built. The error wrapping this one lists every problem.
	ErrReloadConfigInvalid = errors.New("the configuration cannot be reloaded")

	// An earlier reload has been accepted and is not applied yet.
	ErrReloadInProgress = errors.New("a reload is already waiting to be applied")

	// Nothing that can reload the configuration was given to the provider.
	ErrReloadUnavailable = errors.New("reloading the configuration is not available")
)

// RequestReload checks the configuration source again and, when it is valid,
// asks for it to replace the running one. It returns once the new configuration
// is accepted, not when it is running: closing the running core, which includes
// the API that asked, happens afterwards. A configuration with errors changes
// nothing and the error lists them.
func (p *Provider) RequestReload() error {
	if p.requestReload == nil {
		return ErrReloadUnavailable
	}
	p.logger.Info("Reload requested")
	return p.requestReload()
}
