package traffic

import (
	"encoding/json"
	"net/http"

	"github.com/we-apranik/apranik/module/api/clash/internal/stream"
	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

func Register(router *web.Router, provider spec.APICoreProvider, hub *stream.Hub) {
	handler := handler{provider: provider, hub: hub}
	router.Handle("/traffic", web.Methods{"GET": handler.serve})
}

type handler struct {
	provider spec.APICoreProvider
	hub      *stream.Hub
}

type body struct {
	Up        uint64 `json:"up"`
	Down      uint64 `json:"down"`
	UpTotal   uint64 `json:"upTotal"`
	DownTotal uint64 `json:"downTotal"`
}

func (h handler) serve(w http.ResponseWriter, r *http.Request) {
	previous, err := h.provider.TotalTraffic()
	if err != nil {
		web.WriteProviderError(w, err)
		return
	}
	h.hub.Serve(w, r, func(messages *stream.Stream) {
		messages.Every(func() ([]byte, bool) {
			current, err := h.provider.TotalTraffic()
			if err != nil {
				return nil, false
			}
			message, _ := json.Marshal(body{
				Up:        difference(current.UplinkBytes, previous.UplinkBytes),
				Down:      difference(current.DownlinkBytes, previous.DownlinkBytes),
				UpTotal:   current.UplinkBytes,
				DownTotal: current.DownlinkBytes,
			})
			previous = current
			return message, true
		})
	})
}

func difference(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}
