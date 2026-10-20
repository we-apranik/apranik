package apicoreprovider

import "github.com/we-apranik/apranik/spec"

func (p *Provider) RequestReload() error {
	if p.requestReload == nil {
		return spec.ErrAPIReloadUnavailable
	}
	p.logger.Info("Reload requested")
	return p.requestReload()
}
