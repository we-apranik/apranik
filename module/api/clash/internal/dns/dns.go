package dns

import (
	"errors"
	"net/http"
	"strings"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

func Register(router *web.Router, provider spec.APICoreProvider) {
	handler := handler{provider: provider}
	router.Handle("/dns/query", web.Methods{"GET": handler.query})
	router.Handle("/cache/dns/flush", web.Methods{"POST": handler.flush})
}

type handler struct{ provider spec.APICoreProvider }

type questionBody struct {
	Name   string `json:"Name"`
	Qtype  int    `json:"Qtype"`
	Qclass int    `json:"Qclass"`
}

type recordBody struct {
	Name string `json:"name"`
	Type int    `json:"type"`
	TTL  uint32 `json:"TTL"`
	Data string `json:"data"`
}

type answerBody struct {
	Status   int            `json:"Status"`
	Question []questionBody `json:"Question"`
	Server   string         `json:"Server,omitempty"`
	Answer   []recordBody   `json:"Answer,omitempty"`
}

var typeNumbers = map[string]int{"A": 1, "AAAA": 28}

func (h handler) query(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	recordType := strings.ToUpper(r.URL.Query().Get("type"))
	if recordType == "" {
		recordType = "A"
	}

	answer, err := h.provider.QueryDNS(r.Context(), name, recordType)
	switch {
	case errors.Is(err, spec.ErrAPIInvalidDNSQuery):
		web.WriteError(w, http.StatusBadRequest, "%v", err)
		return
	case err != nil:
		web.WriteError(w, http.StatusServiceUnavailable, "the lookup failed: %v", err)
		return
	}

	body := answerBody{
		Question: []questionBody{{Name: strings.TrimSuffix(name, ".") + ".", Qtype: typeNumbers[recordType], Qclass: 1}},
		Server:   answer.ServerTag,
	}
	for _, record := range answer.Records {
		body.Answer = append(body.Answer, recordBody{
			Name: record.Name, Type: typeNumbers[record.Type], TTL: uint32(record.TTL.Seconds()), Data: record.Data,
		})
	}
	web.WriteJSON(w, http.StatusOK, body)
}

func (h handler) flush(w http.ResponseWriter, r *http.Request) {
	if err := h.provider.FlushDNSCache(); err != nil {
		web.WriteError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	web.WriteNoContent(w)
}
