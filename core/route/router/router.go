// Package router decides which outbound serves each request. The Router
// keeps no state, so any caller may ask it at any time: the handler asks once
// per TCP connection and once per new destination in a UDP session. There are
// no rules yet: every request takes the default outbound.
package router

import (
	"context"
	"errors"

	"github.com/The-Apranik/Apranik/spec"
)

// There are no rules yet: every request takes the default outbound. The seam
// is what lets a rule engine land without touching a caller.
type Router struct {
	defaultOutbound spec.Outbound
}

func New(defaultOutbound spec.Outbound) *Router {
	return &Router{defaultOutbound: defaultOutbound}
}

func (r *Router) Route(_ context.Context, _ spec.Metadata) (spec.Outbound, error) {
	if r.defaultOutbound == nil {
		return nil, errors.New("router: no outbound is configured")
	}
	return r.defaultOutbound, nil
}
