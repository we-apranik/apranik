package spec

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// APIModule is a way for another program to read and drive a running Apranik:
// the Clash API, later an Xray-compatible gRPC API. Like every kind, the module
// is the settings and builds the server from them. The settings carry the
// address the server listens on, so core never opens a socket for it.
//
// Validate and Warnings must not listen, dial or read files; the server does
// that in Start.
type APIModule interface {
	Config
	Info() ModuleInfo
	NewAPIServer() (APIServer, error)
}

// APIServer serves one API. Start binds and returns once the server is
// listening, so a taken port fails startup; serving continues in goroutines the
// server owns, and ends when Close is called. Close stops serving, ends every
// open request, such as a log stream, and waits for them. coreProvider is the
// only way the server reaches the running core.
type APIServer interface {
	Start(ctx context.Context, coreProvider APICoreProvider) error
	Close() error
}

// APICoreProvider is everything an API may ask of the running core. Core
// implements it once for every API. Each method does one thing and returns one
// kind of data. Slices it returns are read-only.
//
// A method that reads statistics returns ErrAPIStatisticsDisabled when the
// configuration counts nothing.
type APICoreProvider interface {
	ApranikVersion() string

	// The log level the configuration sets.
	ConfiguredLogLevel() slog.Level

	// Statistics: bytes counted above the proxy layer, TCP and UDP together,
	// or one network.
	TotalTraffic() (APITrafficTotal, error)
	TotalTCPTraffic() (APITrafficTotal, error)
	TotalUDPTraffic() (APITrafficTotal, error)

	// Every live conversation, oldest first; a UDP entry is one UDP session's
	// traffic to one destination. Also ErrAPILiveConversationsDisabled.
	LiveConversations() ([]APILiveConversation, error)

	// CloseLiveConversation asks one live conversation to end; closing a UDP entry ends
	// its whole UDP session. It returns once the close is requested, not when
	// the connection has left the list. ErrAPINoSuchLiveConversation,
	// ErrAPILiveConversationsDisabled.
	CloseLiveConversation(id string) error

	// CloseAllLiveConversations asks every live conversation to end, in one pass.
	// One that begins while it runs may be missed. ErrAPILiveConversationsDisabled.
	CloseAllLiveConversations() error

	// SubscribeToLog sees records from now on, at minimumLevel or above, with
	// room for bufferedRecords of them (at least one). A slow client loses
	// records and then finds a notice in the stream saying how many.
	SubscribeToLog(minimumLevel slog.Level, bufferedRecords int) (APILogSubscription, error)

	// The path of the n-th file log output, absolute. ErrAPINoLogFileOutput
	// when there is none.
	LogFilePath(outputIndex int) (string, error)

	// Outbounds, in configuration order, groups included.
	OutboundTags() []string
	OutboundState(outboundTag string) (APIOutboundState, error)

	// The outbound used when no rule decides: the configured default action's,
	// else the first. Empty when it blocks.
	DefaultOutboundTag() string

	// One request through the outbound, never routed. Core has no defaults: the
	// module applies its own and passes every value. expectedStatus is codes and
	// ranges, "204", "200,204" or "200-299". ErrAPINoSuchOutbound;
	// ErrAPIInvalidDelayTest (a missing or invalid URL, a scheme other than http
	// or https, a zero timeout, a missing or unparsable expectedStatus); a
	// timeout is an error wrapping context.DeadlineExceeded; a status outside
	// expectedStatus is a plain error.
	MeasureDelay(ctx context.Context, outboundTag, rawURL string, timeout time.Duration, expectedStatus string) (time.Duration, error)

	// SelectMember makes memberTag the member the selector uses for new
	// connections, and closes the live conversations that went through the
	// member it replaced. ErrAPINoSuchOutbound, ErrAPIOutboundNotSelectable,
	// ErrAPINoSuchMember.
	SelectMember(selectorTag, memberTag string) error

	// One lookup of name through the resolver, by its DNS rules and cache, as
	// an API client would ask for it. recordType is "A" or "AAAA"; anything else
	// is ErrAPIInvalidDNSQuery, as is a name that is not a valid domain. A name
	// without records of that type gives an answer with no records; a lookup
	// that failed is a plain error.
	QueryDNS(ctx context.Context, name, recordType string) (APIDNSAnswer, error)

	// FlushDNSCache forgets every cached DNS answer. Nothing to forget is not
	// an error.
	FlushDNSCache() error

	// The routing rules in the order they are evaluated, then the default
	// action as the last entry.
	RoutingRules() ([]APIRoutingRule, error)

	// RequestReload checks the configuration source again and, when it is valid,
	// asks for it to replace the running one. It returns once the new
	// configuration is accepted, not once it is running: this API is closed and
	// started again with the rest. ErrAPIInvalidConfiguration (the error wrapping
	// it lists every problem; nothing changes), ErrAPIReloadInProgress,
	// ErrAPIReloadUnavailable.
	RequestReload() error

	// The names of an inbound's users, in the order they were added, for an
	// inbound whose protocol can change its users. ErrAPINoSuchInbound: the tag
	// is unknown, or the protocol cannot change its users (the error says which).
	ListInboundUsers(inboundTag string) ([]string, error)

	// AddInboundUser adds a user, a JSON object as the protocol writes one in its
	// config, and returns its name. It applies from the next handshake. The
	// protocol's own errors come through as they are: ErrInvalidUser,
	// ErrUserExists. ErrAPINoSuchInbound.
	AddInboundUser(inboundTag string, user json.RawMessage) (name string, err error)

	// RemoveInboundUser removes the user and ends their live conversations on
	// that inbound; the same name on another inbound keeps working. When live
	// conversations are not tracked, the open ones stay and the user is still
	// removed. ErrNoSuchUser, ErrAPINoSuchInbound.
	RemoveInboundUser(inboundTag, name string) error
}

// Bytes counted above the proxy layer. Which networks they cover is the
// method's: TotalTraffic, TotalTCPTraffic or TotalUDPTraffic.
type APITrafficTotal struct {
	UplinkBytes   uint64
	DownlinkBytes uint64
}

// One live conversation, or one live UDP entry.
type APILiveConversation struct {
	ID         string
	Network    Network
	InboundTag string

	// The inbound's protocol as the registry names it: "socks5", "http",
	// "trojan", or "mixed".
	InboundProtocol string
	User            string
	Source          Endpoint
	Destination     Destination // a domain stays a domain

	// The name a sniffer found in the payload, when a routing rule asked for a
	// sniff and it found one; empty otherwise, always for UDP. It never replaces
	// Destination.
	SniffedDomain string
	StartedAt     time.Time

	// The outbound that carried it, then the ones beneath it when it is a group's.
	OutboundTags []string

	// Where the router decided: "routing.rules[3]" or "default".
	RuleLocation string

	UplinkBytes   uint64
	DownlinkBytes uint64
}

// What one DNS lookup found. ServerTag is the DNS server that answered, the
// resolver's own when the answer came from its cache.
type APIDNSAnswer struct {
	ServerTag string
	Records   []APIDNSRecord
}

type APIDNSRecord struct {
	Name string
	Type string // "A" or "AAAA"
	TTL  time.Duration
	Data string // the address
}

type APIRoutingRuleAction string

const (
	APIRoutingRuleActionRoute   APIRoutingRuleAction = "route"
	APIRoutingRuleActionBlock   APIRoutingRuleAction = "block"
	APIRoutingRuleActionSniff   APIRoutingRuleAction = "sniff"
	APIRoutingRuleActionResolve APIRoutingRuleAction = "resolve"
)

// One routing rule, or the default action when Location is "default".
type APIRoutingRule struct {
	// Where the rule sits in the configuration: "routing.rules[3]" or "default".
	Location string

	Action APIRoutingRuleAction

	// The conditions as text, such as "domain_suffix=a.com,b.com port=443":
	// fields all hold, a field's values are alternatives. Empty for the default.
	Match string

	// The outbound a route action sends to; empty for the other actions.
	OutboundTag string

	// The DNS server a resolve action asks; empty when the DNS rules choose,
	// and for the other actions.
	DNSServerTag string

	// How many routing decisions this rule made. Only meaningful when
	// HitsCounted is true, that is when statistics are on.
	Hits        uint64
	HitsCounted bool
}

// What a client reads from a log subscription. Cancel is safe to call twice;
// Records closes after Cancel or when core shuts down. When a client falls
// behind, core puts one notice record into the stream (level warn, attribute
// "dropped_records" holding how many, a message saying why and how big the
// buffer was), so the gap is shown where it happened and a module only
// forwards records.
type APILogSubscription interface {
	Records() <-chan slog.Record
	Cancel()
}

type APIOutboundKind string

const (
	APIOutboundKindProxy    APIOutboundKind = "proxy"
	APIOutboundKindFreedom  APIOutboundKind = "freedom"
	APIOutboundKindURLTest  APIOutboundKind = "urltest"
	APIOutboundKindFallback APIOutboundKind = "fallback"
	APIOutboundKindSelector APIOutboundKind = "selector"
)

type APIOutboundState struct {
	Tag          string
	Kind         APIOutboundKind
	ProtocolName string // for APIOutboundKindProxy: "trojan", "socks5"; empty otherwise
	CarriesUDP   bool

	// Nil unless Kind is urltest, fallback or selector.
	Group *APIGroupState
}

// What only a group has. CurrentMember is the selector's choice, the member
// urltest's probes favour, or the first member a fallback tries.
type APIGroupState struct {
	MemberTags    []string // in configuration order
	CurrentMember string
}
