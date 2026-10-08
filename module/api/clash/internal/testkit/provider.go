// Package testkit is what the tests of the Clash endpoint packages share: a fake
// provider the test fills in, a server that mounts one family of endpoints, and
// helpers to call it. Only tests import it.
package testkit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// Provider answers from fields a test sets before it starts the server;
// a method with no function behind it returns the zero value.
type Provider struct {
	Version  string
	LogLevel slog.Level

	TrafficFunc       func() (spec.APITrafficTotal, error)
	ConversationsFunc func() ([]spec.APILiveConversation, error)
	CloseOneFunc      func(id string) error
	CloseAllFunc      func() error
	SubscribeFunc     func(level slog.Level, buffered int) (spec.APILogSubscription, error)
	LogFilePathFunc   func(index int) (string, error)
	Tags              []string
	States            map[string]spec.APIOutboundState
	DefaultTag        string
	MeasureFunc       func(ctx context.Context, tag, rawURL string, timeout time.Duration, expectedStatus string) (time.Duration, error)
	SelectMemberFunc  func(selectorTag, memberTag string) error
	QueryDNSFunc      func(ctx context.Context, name, recordType string) (spec.APIDNSAnswer, error)
	FlushDNSFunc      func() error
	RoutingRulesFunc  func() ([]spec.APIRoutingRule, error)
	RequestReloadFunc func() error
}

var _ spec.APICoreProvider = (*Provider)(nil)

func (f *Provider) ApranikVersion() string         { return f.Version }
func (f *Provider) ConfiguredLogLevel() slog.Level { return f.LogLevel }
func (f *Provider) TotalTraffic() (spec.APITrafficTotal, error) {
	if f.TrafficFunc == nil {
		return spec.APITrafficTotal{}, nil
	}
	return f.TrafficFunc()
}
func (f *Provider) TotalTCPTraffic() (spec.APITrafficTotal, error) { return f.TotalTraffic() }
func (f *Provider) TotalUDPTraffic() (spec.APITrafficTotal, error) { return f.TotalTraffic() }

func (f *Provider) LiveConversations() ([]spec.APILiveConversation, error) {
	if f.ConversationsFunc == nil {
		return nil, nil
	}
	return f.ConversationsFunc()
}

func (f *Provider) CloseLiveConversation(id string) error {
	if f.CloseOneFunc == nil {
		return nil
	}
	return f.CloseOneFunc(id)
}

func (f *Provider) CloseAllLiveConversations() error {
	if f.CloseAllFunc == nil {
		return nil
	}
	return f.CloseAllFunc()
}

func (f *Provider) SubscribeToLog(level slog.Level, buffered int) (spec.APILogSubscription, error) {
	return f.SubscribeFunc(level, buffered)
}

func (f *Provider) LogFilePath(index int) (string, error) {
	if f.LogFilePathFunc == nil {
		return "", spec.ErrAPINoLogFileOutput
	}
	return f.LogFilePathFunc(index)
}

func (f *Provider) OutboundTags() []string { return f.Tags }

func (f *Provider) OutboundState(tag string) (spec.APIOutboundState, error) {
	state, found := f.States[tag]
	if !found {
		return spec.APIOutboundState{}, spec.ErrAPINoSuchOutbound
	}
	return state, nil
}

func (f *Provider) DefaultOutboundTag() string { return f.DefaultTag }

func (f *Provider) MeasureDelay(ctx context.Context, tag, rawURL string, timeout time.Duration, expectedStatus string) (time.Duration, error) {
	return f.MeasureFunc(ctx, tag, rawURL, timeout, expectedStatus)
}

func (f *Provider) SelectMember(selectorTag, memberTag string) error {
	if f.SelectMemberFunc == nil {
		return nil
	}
	return f.SelectMemberFunc(selectorTag, memberTag)
}

func (f *Provider) QueryDNS(ctx context.Context, name, recordType string) (spec.APIDNSAnswer, error) {
	return f.QueryDNSFunc(ctx, name, recordType)
}

func (f *Provider) FlushDNSCache() error {
	if f.FlushDNSFunc == nil {
		return nil
	}
	return f.FlushDNSFunc()
}

func (f *Provider) RoutingRules() ([]spec.APIRoutingRule, error) {
	if f.RoutingRulesFunc == nil {
		return nil, nil
	}
	return f.RoutingRulesFunc()
}

func (f *Provider) RequestReload() error {
	if f.RequestReloadFunc == nil {
		return nil
	}
	return f.RequestReloadFunc()
}

// LogSubscription is a log subscription the test feeds through Feed; Cancelled
// is closed when the code under test cancels it.
type LogSubscription struct {
	Feed      chan slog.Record
	Cancelled chan struct{}
	once      sync.Once
}

func NewLogSubscription() *LogSubscription {
	return &LogSubscription{Feed: make(chan slog.Record, 16), Cancelled: make(chan struct{})}
}

func (s *LogSubscription) Records() <-chan slog.Record { return s.Feed }
func (s *LogSubscription) Cancel()                     { s.once.Do(func() { close(s.Cancelled) }) }
