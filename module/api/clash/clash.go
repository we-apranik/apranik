// Package clash is the Clash-compatible REST and WebSocket API.
package clash

import (
	"time"

	"github.com/we-apranik/apranik/spec"
)

const moduleName = "clash"

type Module struct {
	Settings
}

func NewAPIModule() spec.APIModule { return &Module{} }

var _ spec.APIModule = (*Module)(nil)

func (m *Module) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: moduleName, Description: "Clash API: REST and WebSocket for dashboards and apps"}
}

func (m *Module) NewAPIServer() (spec.APIServer, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return newServer(m.Settings, time.Second), nil
}
