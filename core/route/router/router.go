// Package router decides where each request goes. Rules are
// compiled once, with every tag already turned into the outbound or dataset it
// names, and then only read: one Router serves every inbound at once. What
// costs work to find out, such as a sniff, it asks the traffic informant for,
// and only when a rule's action says so.
package router

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/spec"
)

const defaultRuleName = "default"

type Router struct {
	rules           []compiledRule
	defaultDecision component.Decision
}

type compiledRule struct {
	name      string // "routing.rules[3]"
	condition condition
	action    compiledAction
}

type actionKind int

const (
	actionRoute actionKind = iota
	actionBlock
	actionSniff
)

type compiledAction struct {
	kind         actionKind
	outbound     component.Outbound
	sniffers     []spec.Sniffer
	sniffTimeout time.Duration
}

// Config is what New compiles: the routing blueprint, and everything its tags
// name, already built.
type Config struct {
	Routing   blueprint.Routing
	Outbounds map[string]component.Outbound
	Datasets  map[string]*dataset.Dataset
}

func New(config Config) (*Router, error) {
	compiler := ruleCompiler{outbounds: config.Outbounds, datasets: config.Datasets}

	router := &Router{}
	var errs []error
	for index, rule := range config.Routing.Rules {
		name := fmt.Sprintf("routing.rules[%d]", index)
		condition, conditionErr := compiler.compileMatch(rule.Match)
		action, actionErr := compiler.compileAction(rule.Action)
		if err := errors.Join(conditionErr, actionErr); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		router.rules = append(router.rules, compiledRule{name: name, condition: condition, action: action})
	}

	defaultAction, err := compiler.compileAction(config.Routing.DefaultAction)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("routing.default_action: %w", err))
	case defaultAction.kind == actionSniff:
		errs = append(errs, errors.New("routing.default_action: must route or block"))
	default:
		router.defaultDecision = component.Decision{Outbound: defaultAction.outbound, Rule: defaultRuleName}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("router: %w", errors.Join(errs...))
	}
	return router, nil
}

// NewWithDefaultOutbound is the router for a config with no routing section:
// every request takes one outbound.
func NewWithDefaultOutbound(outbound component.Outbound) *Router {
	return &Router{defaultDecision: component.Decision{Outbound: outbound, Rule: defaultRuleName}}
}

func (r *Router) Route(ctx context.Context, informant component.RouterTrafficInformant) (component.Decision, error) {
	request := newRoutedRequest(informant.Metadata())
	for index := range r.rules {
		rule := &r.rules[index]
		if !rule.condition.matches(&request) {
			continue
		}
		switch rule.action.kind {
		case actionSniff:
			request.setSniffed(informant.Sniff(ctx, rule.action.sniffers, rule.action.sniffTimeout))
		case actionRoute:
			return component.Decision{Outbound: rule.action.outbound, Rule: rule.name}, nil
		case actionBlock:
			return component.Decision{Rule: rule.name}, nil
		}
	}
	return r.defaultDecision, nil
}
