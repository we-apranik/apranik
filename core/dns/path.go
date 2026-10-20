package dns

import (
	"context"
	"fmt"
	"strings"
)

type lookupPathKey struct{}

type lookupPathStep struct {
	kind     string // "dns server" or "outbound"
	tag      string
	previous *lookupPathStep
}

func ContextWithOutboundInLookupPath(ctx context.Context, outboundTag string) context.Context {
	return contextWithLookupPathStep(ctx, "outbound", outboundTag)
}

func contextWithDNSServerInLookupPath(ctx context.Context, serverTag string) context.Context {
	return contextWithLookupPathStep(ctx, "dns server", serverTag)
}

func contextWithLookupPathStep(ctx context.Context, kind, tag string) context.Context {
	previous, _ := ctx.Value(lookupPathKey{}).(*lookupPathStep)
	return context.WithValue(ctx, lookupPathKey{}, &lookupPathStep{kind: kind, tag: tag, previous: previous})
}

type LoopError struct {
	Name string

	Path []string
}

func (e *LoopError) Error() string {
	return fmt.Sprintf("dns: resolving %q loops: %s; give one of them a dns_server that does not go through the others",
		e.Name, strings.Join(e.Path, " → "))
}

func loopThrough(ctx context.Context, name, serverTag string) error {
	step, _ := ctx.Value(lookupPathKey{}).(*lookupPathStep)
	var reversed []string
	found := false
	for ; step != nil; step = step.previous {
		reversed = append(reversed, fmt.Sprintf("%s %q", step.kind, step.tag))
		if step.kind == "dns server" && step.tag == serverTag {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	path := make([]string, 0, len(reversed)+1)
	for index := len(reversed) - 1; index >= 0; index-- {
		path = append(path, reversed[index])
	}
	path = append(path, fmt.Sprintf("dns server %q", serverTag))
	return &LoopError{Name: name, Path: path}
}
