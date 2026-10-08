// Package rules serves /rules: Apranik's routing rules in Clash's envelope.
package rules

import (
	"net/http"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
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

	// Not Clash's: how many routing decisions the rule made. Absent when
	// statistics are off.
	HitCount *uint64 `json:"hitCount,omitempty"`
}

// list answers Clash's envelope with Apranik's content. A Clash rule is one
// condition; an Apranik rule is several, so type is the rule's action (route,
// block, sniff, resolve; "default" for the default action), payload is its
// conditions as text, and proxy is the outbound it routes to.
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
