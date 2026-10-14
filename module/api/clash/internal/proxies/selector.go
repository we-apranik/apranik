package proxies

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

type selectBody struct {
	Name string `json:"name"`
}

// selectMember switches a selector. Only a selector can be switched: the other
// groups choose by themselves, and GLOBAL is a view.
func (h handler) selectMember(w http.ResponseWriter, r *http.Request) {
	selectorTag := r.PathValue("name")
	state, err := h.provider.OutboundState(selectorTag)
	if errors.Is(err, spec.ErrAPINoSuchOutbound) && selectorTag == globalGroupName {
		web.WriteError(w, http.StatusBadRequest, "%s is a read-only view of every outbound; switch a selector group instead", globalGroupName)
		return
	}
	if err != nil {
		web.WriteProviderError(w, err)
		return
	}
	if state.Kind != spec.APIOutboundKindSelector {
		web.WriteError(w, http.StatusBadRequest, "%s is %s, not a selector: %s", selectorTag, describeKind(state.Kind), whoChoosesFor(state.Kind))
		return
	}

	var body selectBody
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Name == "" {
		web.WriteError(w, http.StatusBadRequest, `the body must be {"name": "<member tag>"}`)
		return
	}

	err = h.provider.SelectMember(selectorTag, body.Name)
	switch {
	case err == nil:
		web.WriteNoContent(w)
	case errors.Is(err, spec.ErrAPINoSuchMember):
		web.WriteError(w, http.StatusBadRequest, "%s has no member %q; its members are %s", selectorTag, body.Name, strings.Join(state.Group.MemberTags, ", "))
	case errors.Is(err, spec.ErrAPIOutboundNotSelectable):
		web.WriteError(w, http.StatusBadRequest, "%s is not a selector", selectorTag)
	default:
		web.WriteProviderError(w, err)
	}
}

func describeKind(kind spec.APIOutboundKind) string {
	if kind == spec.APIOutboundKindProxy || kind == spec.APIOutboundKindFreedom {
		return "an outbound, not a group"
	}
	return "a " + string(kind) + " group"
}

func whoChoosesFor(kind spec.APIOutboundKind) string {
	if kind == spec.APIOutboundKindURLTest || kind == spec.APIOutboundKindFallback {
		return "it chooses its member by itself"
	}
	return "only a selector group can be switched"
}
