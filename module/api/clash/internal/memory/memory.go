package memory

import (
	"encoding/json"
	"net/http"
	"runtime"

	"github.com/we-apranik/apranik/module/api/clash/internal/stream"
	"github.com/we-apranik/apranik/module/api/clash/internal/web"
)

func Register(router *web.Router, hub *stream.Hub) {
	router.Handle("/memory", web.Methods{"GET": func(w http.ResponseWriter, r *http.Request) { serve(hub, w, r) }})
}

type body struct {
	InUse   uint64 `json:"inuse"`
	OSLimit uint64 `json:"oslimit"`
}

func serve(hub *stream.Hub, w http.ResponseWriter, r *http.Request) {
	hub.Serve(w, r, func(messages *stream.Stream) {
		messages.Every(func() ([]byte, bool) {
			message, _ := json.Marshal(body{InUse: InUse()})
			return message, true
		})
	})
}

func InUse() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys - stats.HeapReleased
}
