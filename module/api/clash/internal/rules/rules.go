package rules

import (
	"net/http"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

func Register(router *web.Router, provider spec.APICoreProvider) {
	handler := handler{provider: provider}
	router.Handle("/rules", web.Methods{"GET": handler.list})
}

type handler struct{ provider spec.APICoreProvider }

type ruleBody struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`

	HitCount *uint64 `json:"hitCount,omitempty"`
}

func (h handler) list(w http.ResponseWriter, r *http.Request) {
	rules, err := h.provider.RoutingRules()
	if err != nil {
		web.WriteProviderError(w, err)
		return
	}
	bodies := make([]ruleBody, len(rules))
	for index, rule := range rules {
		body := ruleBody{Index: index, Type: string(rule.Action), Payload: rule.Match, Proxy: rule.OutboundTag}
		if rule.Action == spec.APIRoutingRuleActionResolve {
			body.Proxy = rule.DNSServerTag
		}
		if rule.Location == "default" {
			body.Type = "default"
		}
		if rule.HitsCounted {
			hits := rule.Hits
			body.HitCount = &hits
		}
		bodies[index] = body
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"rules": bodies})
}
