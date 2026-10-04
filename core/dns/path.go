package dns

import (
	"context"
	"fmt"
	"strings"
)

// A lookup can need itself: DNS server "google" goes through outbound
// "proxy", whose own server name has to be resolved, possibly by "google".
// Each lookup's context carries the DNS servers and outbounds it is waiting
// on, so a lookup that would wait on itself fails at once instead of hanging.
// Only core reads it; modules pass the context along without knowing.

type lookupPathKey struct{}

type lookupPathStep struct {
	kind     string // "dns server" or "outbound"
	tag      string
	previous *lookupPathStep
}

// ContextWithOutboundInLookupPath records that the work under ctx goes through
// the outbound tagged tag, so a loop through it can be named.
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

// LoopError is a lookup that needs a DNS server already waiting on it.
type LoopError struct {
	Name string

	// The DNS servers and outbounds on the loop, from the first to the one
	// that needs the server again, then that server.
	Path []string
}

func (e *LoopError) Error() string {
	return fmt.Sprintf("dns: resolving %q loops: %s; give one of them a dns_server that does not go through the others",
		e.Name, strings.Join(e.Path, " → "))
}

// loopThrough returns a LoopError when serverTag is already in ctx's path.
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
