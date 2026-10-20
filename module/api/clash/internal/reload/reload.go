package reload

import (
	"errors"
	"net/http"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

func Register(router *web.Router, provider spec.APICoreProvider) {
	handler := handler{provider: provider}
	router.Handle("/reload", web.Methods{"POST": handler.reload})
}

type handler struct{ provider spec.APICoreProvider }

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
