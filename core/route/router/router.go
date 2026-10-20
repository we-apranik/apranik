// Package router decides where each request goes.
package router

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/route/dataset"
	"github.com/we-apranik/apranik/spec"
)

// DefaultRuleLocation is what Decision.Rule holds when no rule decided.
const DefaultRuleLocation = "default"

func RuleLocation(index int) string { return fmt.Sprintf("routing.rules[%d]", index) }

type Router struct {
	rules           []compiledRule
	defaultDecision component.Decision
}

type compiledRule struct {
	location  string // RuleLocation(3): "routing.rules[3]"
	condition condition
	action    compiledAction
}

type actionKind int

const (
	actionRoute actionKind = iota
	actionBlock
	actionSniff
	actionResolve
)

type compiledAction struct {
	kind         actionKind
	outbound     component.Outbound
	sniffers     []spec.Sniffer
	sniffTimeout time.Duration

	resolveServer string
}

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
		location := RuleLocation(index)
		condition, conditionErr := compiler.compileMatch(rule.Match)
		action, actionErr := compiler.compileAction(rule.Action)
		if err := errors.Join(conditionErr, actionErr); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", location, err))
			continue
		}
		router.rules = append(router.rules, compiledRule{location: location, condition: condition, action: action})
	}

	defaultAction, err := compiler.compileAction(config.Routing.DefaultAction)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("routing.default_action: %w", err))
	case defaultAction.kind == actionSniff, defaultAction.kind == actionResolve:
		errs = append(errs, errors.New("routing.default_action: must route or block"))
	default:
		router.defaultDecision = component.Decision{Outbound: defaultAction.outbound, Rule: DefaultRuleLocation}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("router: %w", errors.Join(errs...))
	}
	return router, nil
}

func NewWithDefaultOutbound(outbound component.Outbound) *Router {
	return &Router{defaultDecision: component.Decision{Outbound: outbound, Rule: DefaultRuleLocation}}
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
		case actionResolve:
			request.setResolved(informant.Resolve(ctx, rule.action.resolveServer))
		case actionRoute:
			return component.Decision{Outbound: rule.action.outbound, Rule: rule.location}, nil
		case actionBlock:
			return component.Decision{Rule: rule.location}, nil
		}
	}
	return r.defaultDecision, nil
}
