package dns

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeZone answers like a recursive resolver from a fixed table.
type fakeZone struct {
	addresses map[string][]netip.Addr // name with trailing dot → addresses
	aliases   map[string]string       // name → CNAME target
	ttl       uint32
	failWith  error

	queries atomic.Int64

	// Holds every query until it is closed, when set.
	release chan struct{}

	mu        sync.Mutex
	questions []dnsmessage.Question
}

func newFakeZone() *fakeZone {
	return &fakeZone{addresses: map[string][]netip.Addr{}, aliases: map[string]string{}, ttl: 60}
}

func (z *fakeZone) Query(ctx context.Context, queryMessage []byte) ([]byte, error) {
	z.queries.Add(1)
	if z.release != nil {
		select {
		case <-z.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if z.failWith != nil {
		return nil, z.failWith
	}

	var parser dnsmessage.Parser
	header, err := parser.Start(queryMessage)
	if err != nil {
		return nil, err
	}
	question, err := parser.Question()
	if err != nil {
		return nil, err
	}
	z.mu.Lock()
	z.questions = append(z.questions, question)
	z.mu.Unlock()

	return z.respond(header.ID, question), nil
}

func (z *fakeZone) respond(id uint16, question dnsmessage.Question) []byte {
	owner := strings.ToLower(question.Name.String())
	var answers []dnsmessage.Resource
	for range 32 {
		target, aliased := z.aliases[owner]
		if !aliased {
			break
		}
		answers = append(answers, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{Name: mustName(owner), Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: z.ttl},
			Body:   &dnsmessage.CNAMEResource{CNAME: mustName(target)},
		})
		owner = target
	}
	known, exists := z.addresses[owner]
	for _, address := range known {
		if question.Type == dnsmessage.TypeA && address.Is4() {
			answers = append(answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: mustName(owner), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: z.ttl},
				Body:   &dnsmessage.AResource{A: address.As4()},
			})
		}
		if question.Type == dnsmessage.TypeAAAA && address.Is6() {
			answers = append(answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: mustName(owner), Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET, TTL: z.ttl},
				Body:   &dnsmessage.AAAAResource{AAAA: address.As16()},
			})
		}
	}

	header := dnsmessage.Header{ID: id, Response: true, RecursionAvailable: true}
	if !exists && len(answers) == 0 {
		header.RCode = dnsmessage.RCodeNameError
	}
	builder := dnsmessage.NewBuilder(nil, header)
	builder.StartQuestions()
	builder.Question(question)
	builder.StartAnswers()
	for _, record := range answers {
		switch body := record.Body.(type) {
		case *dnsmessage.CNAMEResource:
			builder.CNAMEResource(record.Header, *body)
		case *dnsmessage.AResource:
			builder.AResource(record.Header, *body)
		case *dnsmessage.AAAAResource:
			builder.AAAAResource(record.Header, *body)
		}
	}
	message, err := builder.Finish()
	if err != nil {
		panic(err)
	}
	return message
}

func (z *fakeZone) Close() error { return nil }

func mustName(text string) dnsmessage.Name {
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return dnsmessage.MustNewName(text)
}

// newTestResolver attaches one wire server tagged "fake" as the default.
func newTestResolver(t testing.TB, config Config, zone *fakeZone) *Resolver {
	t.Helper()
	resolver := NewResolver(config)
	if err := resolver.AttachServers([]Server{{Tag: "fake", Wire: zone}}, nil, "fake", nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resolver.Close() })
	return resolver
}

var errFakeFailure = errors.New("fake server failure")
