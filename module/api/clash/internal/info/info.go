// Package info serves the endpoints that say what this API is: /, /version and
// /configs.
package info

import (
	"net/http"

	"github.com/we-apranik/apranik/module/api/clash/internal/loglevel"
	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

func Register(router *web.Router, provider spec.APICoreProvider) {
	handlers := handlers{provider: provider}
	router.Handle("/{$}", web.Methods{"GET": handlers.hello})
	router.Handle("/version", web.Methods{"GET": handlers.version})
	router.Handle("/configs", web.Methods{"GET": handlers.configs, "PUT": handlers.notSettable, "PATCH": handlers.notSettable})
}

type handlers struct{ provider spec.APICoreProvider }

func (h handlers) hello(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, map[string]string{"hello": "clash"})
}

// "meta" says the extra endpoints of the Meta family (memory, group delay, rules,
// DNS query) exist, which dashboards look for.
func (h handlers) version(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, map[string]any{"meta": true, "version": "Apranik " + h.provider.ApranikVersion()})
}

func (h handlers) configs(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, map[string]any{
		"mode":      "rule",
		"mode-list": []string{"rule"},
		"log-level": loglevel.Name(h.provider.ConfiguredLogLevel()),
	})
}

func (h handlers) notSettable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "GET")
	web.WriteError(w, http.StatusMethodNotAllowed,
		"this core's mode and ports are not settable through the API; edit the configuration and POST /reload to load it again")
}
