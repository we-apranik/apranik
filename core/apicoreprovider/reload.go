package apicoreprovider

import "github.com/The-Apranik/Apranik/spec"

// RequestReload checks the configuration source again and, when it is valid,
// asks for it to replace the running one. It returns once the new configuration
// is accepted, not when it is running: closing the running core, which includes
// the API that asked, happens afterwards. The function it asks returns the
// spec.ErrAPI... errors itself: a configuration with errors changes nothing and
// the error lists them.
func (p *Provider) RequestReload() error {
	if p.requestReload == nil {
		return spec.ErrAPIReloadUnavailable
	}
	p.logger.Info("Reload requested")
	return p.requestReload()
}
