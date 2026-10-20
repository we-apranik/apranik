package users

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

const (
	singleUserBodyLimit = 16 << 10

	batchBodyLimit = 8 << 20
)

func Register(router *web.Router, provider spec.APICoreProvider) {
	handler := handler{provider: provider}
	router.Handle("/inbounds/{tag}/users", web.Methods{"GET": handler.list, "POST": handler.add})
	router.Handle("/inbounds/{tag}/users/{name}", web.Methods{"GET": handler.get, "DELETE": handler.remove})
	router.Handle("/inbounds/{tag}/users:batchCreate", web.Methods{"POST": handler.batchCreate})
	router.Handle("/inbounds/{tag}/users:batchDelete", web.Methods{"POST": handler.batchDelete})
}

type handler struct{ provider spec.APICoreProvider }

func (h handler) list(w http.ResponseWriter, r *http.Request) {
	names, err := h.provider.ListInboundUsers(r.PathValue("tag"))
	if err != nil {
		writeUserError(w, err)
		return
	}
	if names == nil {
		names = []string{}
	}
	web.WriteJSON(w, http.StatusOK, map[string][]string{"users": names})
}

func (h handler) get(w http.ResponseWriter, r *http.Request) {
	tag, name := r.PathValue("tag"), r.PathValue("name")
	names, err := h.provider.ListInboundUsers(tag)
	if err != nil {
		writeUserError(w, err)
		return
	}
	if !slices.Contains(names, name) {
		web.WriteError(w, http.StatusNotFound, "inbound %q has no user %q", tag, name)
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (h handler) add(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, singleUserBodyLimit)
	if !ok {
		return
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' {
		web.WriteError(w, http.StatusBadRequest, "the body must be one user, a JSON object")
		return
	}

	name, err := h.provider.AddInboundUser(r.PathValue("tag"), body)
	if err != nil {
		writeUserError(w, err)
		return
	}
	web.WriteJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (h handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.provider.RemoveInboundUser(r.PathValue("tag"), r.PathValue("name")); err != nil {
		writeUserError(w, err)
		return
	}
	web.WriteNoContent(w)
}

func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		web.WriteError(w, http.StatusRequestEntityTooLarge, "the body is over %d bytes", limit)
		return nil, false
	case err != nil:
		web.WriteError(w, http.StatusBadRequest, "the body could not be read: %v", err)
		return nil, false
	}
	return body, true
}

func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, spec.ErrAPINoSuchInbound), errors.Is(err, spec.ErrNoSuchUser):
		web.WriteError(w, http.StatusNotFound, "%v", err)
	case errors.Is(err, spec.ErrInvalidUser):
		web.WriteError(w, http.StatusBadRequest, "%v", err)
	case errors.Is(err, spec.ErrUserExists):
		web.WriteError(w, http.StatusConflict, "%v", err)
	default:
		web.WriteProviderError(w, err)
	}
}
