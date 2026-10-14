// Package memory serves /memory, and says how much memory the process holds.
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

// serve sends how much memory the process holds from the operating system and
// has not given back, every tick. There is no limit to report.
func serve(hub *stream.Hub, w http.ResponseWriter, r *http.Request) {
	hub.Serve(w, r, func(messages *stream.Stream) {
		messages.Every(func() ([]byte, bool) {
			message, _ := json.Marshal(body{InUse: InUse()})
			return message, true
		})
	})
}

// InUse is the memory the process has taken from the operating system and not
// given back.
func InUse() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys - stats.HeapReleased
}
