// Package reload serves POST /reload: load the configuration again. It is an
// Apranik extension; Mihomo reloads through PUT /configs.
package reload

import (
	"errors"
	"net/http"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func Register(router *web.Router, provider spec.APICoreProvider) {
	handler := handler{provider: provider}
	router.Handle("/reload", web.Methods{"POST": handler.reload})
}

type handler struct{ provider spec.APICoreProvider }

// reload asks the core to load its configuration source again. The answer says
// whether the configuration was accepted. It is applied afterwards, and this
// server is closed and started again with the rest, so the client reconnects.
func (h handler) reload(w http.ResponseWriter, r *http.Request) {
	err := h.provider.RequestReload()
	switch {
	case err == nil:
		web.WriteJSON(w, http.StatusAccepted, map[string]string{"message": "the configuration was accepted and is being loaded; this API restarts with it"})
	case errors.Is(err, spec.ErrAPIInvalidConfiguration):
		web.WriteError(w, http.StatusBadRequest, "%v", err)
	case errors.Is(err, spec.ErrAPIReloadInProgress):
		web.WriteError(w, http.StatusConflict, "%v", err)
	case errors.Is(err, spec.ErrAPIReloadUnavailable):
		web.WriteError(w, http.StatusNotImplemented, "%v", err)
	default:
		web.WriteError(w, http.StatusInternalServerError, "%v", err)
	}
}
