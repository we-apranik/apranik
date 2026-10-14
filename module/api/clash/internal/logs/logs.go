// Package logs serves /logs: the core's log as it is written.
package logs

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/we-apranik/apranik/module/api/clash/internal/loglevel"
	"github.com/we-apranik/apranik/module/api/clash/internal/stream"
	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

// Register serves /logs; bufferRecords is how far one client may fall behind
// before records are dropped.
func Register(router *web.Router, provider spec.APICoreProvider, hub *stream.Hub, bufferRecords int) {
	handler := handler{provider: provider, hub: hub, bufferRecords: bufferRecords}
	router.Handle("/logs", web.Methods{"GET": handler.serve})
}

type handler struct {
	provider      spec.APICoreProvider
	hub           *stream.Hub
	bufferRecords int
}

type line struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// serve streams the records at or above ?level= (default info), one subscription
// per request. A client that reads too slowly loses records, and the stream says
// how many where it happened.
func (h handler) serve(w http.ResponseWriter, r *http.Request) {
	levelName := r.URL.Query().Get("level")
	if levelName == "" {
		levelName = "info"
	}
	minimumLevel, known := loglevel.Minimum(levelName)
	if !known {
		web.WriteError(w, http.StatusBadRequest, "level is debug, info, warning, error or silent, not %q", levelName)
		return
	}

	subscription, err := h.provider.SubscribeToLog(minimumLevel, h.bufferRecords)
	if err != nil {
		web.WriteProviderError(w, err)
		return
	}
	defer subscription.Cancel()
	h.hub.Serve(w, r, func(messages *stream.Stream) {
		for {
			select {
			case <-messages.Context().Done():
				return
			case record, open := <-subscription.Records():
				if !open {
					return
				}
				message, _ := json.Marshal(line{Type: loglevel.Name(record.Level), Payload: describe(record)})
				if messages.Send(message) != nil {
					return
				}
			}
		}
	})
}

// describe writes a record as its message followed by key=value pairs, the way a
// person reads a log line. A value with a space, a quote or an equals sign is
// quoted.
func describe(record slog.Record) string {
	var text strings.Builder
	text.WriteString(record.Message)
	record.Attrs(func(attribute slog.Attr) bool {
		appendAttribute(&text, "", attribute)
		return true
	})
	return text.String()
}

func appendAttribute(text *strings.Builder, prefix string, attribute slog.Attr) {
	value := attribute.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		groupPrefix := prefix
		if attribute.Key != "" {
			groupPrefix += attribute.Key + "."
		}
		for _, member := range value.Group() {
			appendAttribute(text, groupPrefix, member)
		}
		return
	}
	if attribute.Equal(slog.Attr{}) {
		return
	}
	text.WriteByte(' ')
	text.WriteString(prefix + attribute.Key)
	text.WriteByte('=')
	rendered := value.String()
	if strings.ContainsAny(rendered, " \t\n\"=") || rendered == "" {
		rendered = strconv.Quote(rendered)
	}
	text.WriteString(rendered)
}
