// Package clash is the Clash API: the REST and WebSocket interface
// that Clash dashboards and apps use to read and drive a running core. It is an
// API module: it opens its own listener and reaches the core only through the
// spec.APICoreProvider it is given. Endpoints follow Mihomo's shapes where
// Apranik has the same thing, and say so where it does not (the rules, and
// reloading the configuration, are Apranik's own).
//
// This package is the module's surface: Module and Settings, and the server's
// lifecycle. Each family of endpoints is a package under internal/ (info,
// traffic, memory, connections, proxies, logs, logfile, dns, rules, reload) on
// the shared plumbing in internal/web (router, answers, authentication, CORS)
// and internal/stream (JSON lines and WebSocket frames).
package clash

import (
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// The name the module is registered and written in a config as.
const moduleName = "clash"

// Module is the settings and builds the server from them.
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
