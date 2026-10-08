// Package proxies serves the outbounds: /proxies, switching a selector, and the
// delay tests of one outbound or of a group's members.
package proxies

import (
	"errors"
	"net/http"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

// DelayDefaults are what a delay request gets for the values it leaves out. Core
// has none, so the module passes every value.
type DelayDefaults struct {
	URL            string
	Timeout        time.Duration
	ExpectedStatus string
}

func Register(router *web.Router, provider spec.APICoreProvider, defaults DelayDefaults) {
	handler := handler{provider: provider, defaults: defaults}
	router.Handle("/proxies", web.Methods{"GET": handler.list})
	router.Handle("/proxies/{name}", web.Methods{"GET": handler.one, "PUT": handler.selectMember})
	router.Handle("/proxies/{name}/delay", web.Methods{"GET": handler.delay})
	router.Handle("/group/{name}/delay", web.Methods{"GET": handler.groupDelay})
}

type handler struct {
	provider spec.APICoreProvider
	defaults DelayDefaults
}

// The name dashboards expect for the group of every outbound. It is a view built
// here, not an outbound: a real outbound with this tag wins.
const globalGroupName = "GLOBAL"

type proxyBody struct {
	Type    string     `json:"type"`
	Name    string     `json:"name"`
	UDP     bool       `json:"udp"`
	History []struct{} `json:"history"`

	// Only a group has these.
	Now *string  `json:"now,omitempty"`
	All []string `json:"all,omitempty"`
}

func (h handler) list(w http.ResponseWriter, r *http.Request) {
	tags := h.provider.OutboundTags()
	bodies := make(map[string]proxyBody, len(tags)+1)
	carriesUDP := false
	for _, tag := range tags {
		state, err := h.provider.OutboundState(tag)
		if err != nil {
			web.WriteProviderError(w, err)
			return
		}
		bodies[tag] = proxyOf(state)
		carriesUDP = carriesUDP || state.CarriesUDP
	}
	if _, taken := bodies[globalGroupName]; !taken {
		bodies[globalGroupName] = h.globalGroup(tags, carriesUDP)
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"proxies": bodies})
}

func (h handler) one(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	state, err := h.provider.OutboundState(name)
	if errors.Is(err, spec.ErrAPINoSuchOutbound) && name == globalGroupName {
		web.WriteJSON(w, http.StatusOK, h.globalGroup(h.provider.OutboundTags(), true))
		return
	}
	if err != nil {
		web.WriteProviderError(w, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, proxyOf(state))
}

func proxyOf(state spec.APIOutboundState) proxyBody {
	body := proxyBody{Type: clashType(state), Name: state.Tag, UDP: state.CarriesUDP, History: []struct{}{}}
	if state.Group != nil {
		now := state.Group.CurrentMember
		body.Now = &now
		body.All = append([]string{}, state.Group.MemberTags...)
	}
	return body
}

// The fake group dashboards show first: every outbound, and the default one as
// the current. A request that no rule decides goes to the default outbound, so the
// group is accurate about where traffic goes by default.
func (h handler) globalGroup(tags []string, carriesUDP bool) proxyBody {
	now := h.provider.DefaultOutboundTag()
	if now == "" && len(tags) > 0 {
		now = tags[0]
	}
	return proxyBody{Type: "Fallback", Name: globalGroupName, UDP: carriesUDP, History: []struct{}{}, Now: &now, All: append([]string{}, tags...)}
}

func clashType(state spec.APIOutboundState) string {
	switch state.Kind {
	case spec.APIOutboundKindFreedom:
		return "Direct"
	case spec.APIOutboundKindURLTest:
		return "URLTest"
	case spec.APIOutboundKindFallback:
		return "Fallback"
	case spec.APIOutboundKindSelector:
		return "Selector"
	default:
		return capitalized(state.ProtocolName)
	}
}

func capitalized(text string) string {
	first, size := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError {
		return text
	}
	return string(unicode.ToUpper(first)) + text[size:]
}
