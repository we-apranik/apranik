package group_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/outbound/group"
	"github.com/we-apranik/apranik/shared/httpstatusranges"
	"github.com/we-apranik/apranik/spec"
)

// A member that reaches a test server whatever it is asked for, so a probe
// and a real connection both succeed or fail by the server's answer and the
// member's delay. Real connections are told apart by their destination.
type scriptedMember struct {
	tag           string
	serverAddress string
	networks      []spec.Network
	delay         atomic.Int64
	dialFailure   atomic.Pointer[error]

	probeDials      atomic.Int64
	connectionDials atomic.Int64
}

const realDestinationDomain = "destination.example"

func (m *scriptedMember) Tag() string { return m.tag }
func (m *scriptedMember) Networks() []spec.Network {
	if m.networks != nil {
		return m.networks
	}
	return []spec.Network{spec.NetworkTCP}
}

func (m *scriptedMember) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	isConnection := false
	if domain, isDomain := metadata.Destination.Domain(); isDomain && domain == realDestinationDomain {
		isConnection = true
	} else {
		m.probeDials.Add(1)
	}
	if failure := m.dialFailure.Load(); failure != nil {
		return nil, *failure
	}
	if isConnection {
		m.connectionDials.Add(1)
	}
	time.Sleep(time.Duration(m.delay.Load()))
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", m.serverAddress)
	if err != nil {
		return nil, err
	}
	return connection.(*net.TCPConn), nil
}

type stubDatagram struct{ spec.Datagram }

func (m *scriptedMember) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	if failure := m.dialFailure.Load(); failure != nil {
		return nil, *failure
	}
	m.connectionDials.Add(1)
	return stubDatagram{}, nil
}

func (m *scriptedMember) Close() error { return nil }

func answeringServer(t *testing.T, status int) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server.Listener.Addr().String()
}

func newMember(t *testing.T, tag string, status int) *scriptedMember {
	return &scriptedMember{tag: tag, serverAddress: answeringServer(t, status)}
}

func realConnectionMetadata(t *testing.T) spec.Metadata {
	t.Helper()
	destination, err := spec.NewDestinationFromDomain(realDestinationDomain, 443)
	if err != nil {
		t.Fatal(err)
	}
	return spec.Metadata{Network: spec.NetworkTCP, Destination: destination}
}

func newTestGroup(t *testing.T, picksFastest bool, tolerance time.Duration, members ...*scriptedMember) component.ProbingOutbound {
	t.Helper()
	outbounds := make([]component.Outbound, len(members))
	for index, member := range members {
		outbounds[index] = member
	}
	probe := group.ProbeConfig{URL: "http://probe.example/", Interval: 30 * time.Millisecond, Timeout: 2 * time.Second}
	var groupOutbound component.ProbingOutbound
	var err error
	if picksFastest {
		groupOutbound, err = group.NewURLTest(group.URLTestConfig{Tag: "group", Members: outbounds, Probe: probe, Tolerance: tolerance})
	} else {
		groupOutbound, err = group.NewFallback(group.FallbackConfig{Tag: "group", Members: outbounds, Probe: probe})
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { groupOutbound.Close() })
	return groupOutbound
}

// Which member carried the next real connection.
func memberUsed(t *testing.T, groupOutbound component.Outbound, members ...*scriptedMember) string {
	t.Helper()
	before := make([]int64, len(members))
	for index, member := range members {
		before[index] = member.connectionDials.Load()
	}
	stream, err := groupOutbound.DialStream(context.Background(), realConnectionMetadata(t))
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	stream.Close()
	for index, member := range members {
		if member.connectionDials.Load() > before[index] {
			return member.tag
		}
	}
	t.Fatal("no member carried the connection")
	return ""
}

func eventually(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func TestGroupDialsNothingUntilProbingStarts(t *testing.T) {
	slow, fast := newMember(t, "slow", 204), newMember(t, "fast", 204)
	newTestGroup(t, true, 0, slow, fast)

	time.Sleep(100 * time.Millisecond)
	if slow.probeDials.Load()+fast.probeDials.Load() != 0 {
		t.Error("a groupOutbound probed before StartProbing")
	}
}

func TestGroupUsesListOrderUntilTheFirstRoundEnds(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	groupOutbound := newTestGroup(t, true, 0, first, second)

	if used := memberUsed(t, groupOutbound, first, second); used != "first" {
		t.Errorf("used %q before any probe, want the first member", used)
	}
}

func TestURLTestPrefersTheFastestMember(t *testing.T) {
	slow, fast := newMember(t, "slow", 204), newMember(t, "fast", 204)
	slow.delay.Store(int64(150 * time.Millisecond))
	groupOutbound := newTestGroup(t, true, 20*time.Millisecond, slow, fast)
	groupOutbound.StartProbing(context.Background())

	eventually(t, "the fast member to be preferred", func() bool {
		return memberUsed(t, groupOutbound, slow, fast) == "fast"
	})
}

func TestURLTestStaysOnItsMemberWithinTheTolerance(t *testing.T) {
	current, quicker := newMember(t, "current", 204), newMember(t, "quicker", 204)
	current.delay.Store(int64(60 * time.Millisecond))
	groupOutbound := newTestGroup(t, true, 400*time.Millisecond, current, quicker)
	groupOutbound.StartProbing(context.Background())

	eventually(t, "several rounds", func() bool { return quicker.probeDials.Load() >= 5 })
	if used := memberUsed(t, groupOutbound, current, quicker); used != "current" {
		t.Errorf("used %q, but %q is faster by less than the tolerance", used, quicker.tag)
	}

	current.delay.Store(int64(1500 * time.Millisecond))
	eventually(t, "the switch once the gap passes the tolerance", func() bool {
		return memberUsed(t, groupOutbound, current, quicker) == "quicker"
	})
}

func TestURLTestLeavesAMemberWhoseProbeFails(t *testing.T) {
	broken, working := newMember(t, "broken", 500), newMember(t, "working", 204)
	groupOutbound := newTestGroup(t, true, time.Hour, broken, working)
	groupOutbound.StartProbing(context.Background())

	eventually(t, "the broken member to be left", func() bool {
		return memberUsed(t, groupOutbound, broken, working) == "working"
	})
}

func TestURLTestKeepsItsMemberWhenEveryProbeFails(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	second.delay.Store(int64(100 * time.Millisecond))
	groupOutbound := newTestGroup(t, true, 0, first, second)
	groupOutbound.StartProbing(context.Background())
	eventually(t, "a probe round", func() bool { return second.probeDials.Load() >= 2 })

	refused := errors.New("refused")
	first.dialFailure.Store(&refused)
	second.dialFailure.Store(&refused)
	probes := first.probeDials.Load()
	eventually(t, "failing rounds", func() bool { return first.probeDials.Load() >= probes+3 })

	first.dialFailure.Store(nil)
	if used := memberUsed(t, groupOutbound, first, second); used != "first" {
		t.Errorf("used %q, want the last good member", used)
	}
}

func TestFallbackUsesTheFirstMemberThatPassed(t *testing.T) {
	broken, working, spare := newMember(t, "broken", 500), newMember(t, "working", 204), newMember(t, "spare", 204)
	groupOutbound := newTestGroup(t, false, 0, broken, working, spare)
	groupOutbound.StartProbing(context.Background())

	eventually(t, "the broken member to be skipped", func() bool {
		return memberUsed(t, groupOutbound, broken, working, spare) == "working"
	})
	// A faster later member does not matter: list order decides.
	working.delay.Store(int64(100 * time.Millisecond))
	spare.delay.Store(0)
	time.Sleep(150 * time.Millisecond)
	if used := memberUsed(t, groupOutbound, broken, working, spare); used != "working" {
		t.Errorf("used %q, want list order among members that passed", used)
	}
}

func TestGroupTriesTheNextMemberWhenADialFails(t *testing.T) {
	refusing, working := newMember(t, "refusing", 204), newMember(t, "working", 204)
	refused := errors.New("refused")
	refusing.dialFailure.Store(&refused)
	groupOutbound := newTestGroup(t, false, 0, refusing, working)

	if used := memberUsed(t, groupOutbound, refusing, working); used != "working" {
		t.Errorf("used %q, want the second member after the first refused", used)
	}
}

func TestGroupReportsEveryFailureWhenNoMemberConnects(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	firstFailure, secondFailure := errors.New("first refused"), errors.New("second refused")
	first.dialFailure.Store(&firstFailure)
	second.dialFailure.Store(&secondFailure)
	groupOutbound := newTestGroup(t, false, 0, first, second)

	_, err := groupOutbound.DialStream(context.Background(), realConnectionMetadata(t))
	if !errors.Is(err, firstFailure) || !errors.Is(err, secondFailure) {
		t.Errorf("DialStream: %v, want both members' failures", err)
	}
	_, err = groupOutbound.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if !errors.Is(err, firstFailure) || !errors.Is(err, secondFailure) {
		t.Errorf("CreateDatagram: %v, want both members' failures", err)
	}
}

func TestGroupCreatesDatagramsThroughTheFirstMemberThatWorks(t *testing.T) {
	refusing, working := newMember(t, "refusing", 204), newMember(t, "working", 204)
	refused := errors.New("refused")
	refusing.dialFailure.Store(&refused)
	groupOutbound := newTestGroup(t, false, 0, refusing, working)

	datagram, err := groupOutbound.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if err != nil || datagram == nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	if working.connectionDials.Load() != 1 {
		t.Error("the working member did not carry the datagram")
	}
}

func TestAGroupsDatagramNamesTheMemberThatCarriedIt(t *testing.T) {
	refusing, working := newMember(t, "refusing", 204), newMember(t, "working", 204)
	refused := errors.New("refused")
	refusing.dialFailure.Store(&refused)
	groupOutbound := newTestGroup(t, false, 0, refusing, working)

	datagram, err := groupOutbound.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	if tags := component.TagsOfOutboundsThatCarried(groupOutbound, datagram); !slices.Equal(tags, []string{"group", "working"}) {
		t.Errorf("TagsOfOutboundsThatCarried = %v, want [groupOutbound working]", tags)
	}
}

func TestGroupCarriesOnlyWhatEveryMemberCarries(t *testing.T) {
	both := newMember(t, "both", 204)
	both.networks = []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
	tcpOnly := newMember(t, "tcp-only", 204)
	groupOutbound := newTestGroup(t, false, 0, both, tcpOnly)

	if networks := groupOutbound.Networks(); !slices.Equal(networks, []spec.Network{spec.NetworkTCP}) {
		t.Errorf("Networks() = %v, want [tcp]", networks)
	}
}

func TestClosingAGroupStopsItsProbes(t *testing.T) {
	member := newMember(t, "member", 204)
	groupOutbound := newTestGroup(t, true, 0, member)
	groupOutbound.StartProbing(context.Background())
	eventually(t, "the first probes", func() bool { return member.probeDials.Load() >= 2 })

	if err := groupOutbound.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dials := member.probeDials.Load()
	time.Sleep(150 * time.Millisecond)
	if member.probeDials.Load() != dials {
		t.Error("a groupOutbound kept probing after Close")
	}
	groupOutbound.StartProbing(context.Background())
	time.Sleep(100 * time.Millisecond)
	if member.probeDials.Load() != dials {
		t.Error("a closed groupOutbound started probing again")
	}
}

func TestGroupRefusesAProbeItCannotMake(t *testing.T) {
	member := newMember(t, "member", 204)
	for name, config := range map[string]group.ProbeConfig{
		"not http or https": {URL: "ftp://example.com/"},
		"no host":           {URL: "http:///path"},
		"negative interval": {Interval: -time.Second},
		"negative timeout":  {Timeout: -time.Second},
	} {
		if _, err := group.NewFallback(group.FallbackConfig{Tag: "group", Members: []component.Outbound{member}, Probe: config}); err == nil {
			t.Errorf("%s: NewFallback accepted it", name)
		}
		if _, err := group.NewURLTest(group.URLTestConfig{Tag: "group", Members: []component.Outbound{member}, Probe: config}); err == nil {
			t.Errorf("%s: NewURLTest accepted it", name)
		}
	}
}

func TestGroupRefusesNoMembers(t *testing.T) {
	if _, err := group.NewFallback(group.FallbackConfig{Tag: "group"}); err == nil {
		t.Error("NewFallback accepted a groupOutbound with no members")
	}
	if _, err := group.NewURLTest(group.URLTestConfig{Tag: "group"}); err == nil {
		t.Error("NewURLTest accepted a groupOutbound with no members")
	}
}

func TestAProbeNamesNeitherItselfNorTheClientLibrary(t *testing.T) {
	userAgents := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case userAgents <- request.Header.Values("User-Agent"):
		default:
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	member := &scriptedMember{tag: "member", serverAddress: server.Listener.Addr().String()}
	groupOutbound := newTestGroup(t, true, 0, member)
	groupOutbound.StartProbing(context.Background())

	select {
	case got := <-userAgents:
		if len(got) != 0 {
			t.Errorf("the probe sent User-Agent %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the probe never arrived")
	}
}

func TestAGroupsStreamNamesTheMemberThatCarriedIt(t *testing.T) {
	refusing, working := newMember(t, "refusing", 204), newMember(t, "working", 204)
	refused := errors.New("refused")
	refusing.dialFailure.Store(&refused)
	groupOutbound := newTestGroup(t, false, 0, refusing, working)

	stream, err := groupOutbound.DialStream(context.Background(), realConnectionMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	reporter, reports := stream.(component.CarryingOutboundsReporter)
	if !reports {
		t.Fatal("a groupOutbound's stream does not report which outbound carried it")
	}
	if tags := reporter.CarryingOutboundTags(); !slices.Equal(tags, []string{"working"}) {
		t.Errorf("carrying outbound tags = %v, want [working]", tags)
	}
	if tags := component.TagsOfOutboundsThatCarried(groupOutbound, stream); !slices.Equal(tags, []string{"group", "working"}) {
		t.Errorf("TagsOfOutboundsThatCarried = %v, want [groupOutbound working]", tags)
	}
}

func TestAGroupOfGroupsNamesEveryLevelThatCarriedTheStream(t *testing.T) {
	member := newMember(t, "member", 204)
	inner, err := group.NewFallback(group.FallbackConfig{
		Tag:     "inner",
		Members: []component.Outbound{member},
		Probe:   group.ProbeConfig{URL: "http://probe.example/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := group.NewFallback(group.FallbackConfig{
		Tag:     "outer",
		Members: []component.Outbound{inner},
		Probe:   group.ProbeConfig{URL: "http://probe.example/"},
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := outer.DialStream(context.Background(), realConnectionMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if tags := component.TagsOfOutboundsThatCarried(outer, stream); !slices.Equal(tags, []string{"outer", "inner", "member"}) {
		t.Errorf("tags = %v, want [outer inner member]", tags)
	}
}

func TestAPlainOutboundsCarrierNamesOnlyTheOutbound(t *testing.T) {
	member := newMember(t, "member", 204)
	stream, err := member.DialStream(context.Background(), realConnectionMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if tags := component.TagsOfOutboundsThatCarried(member, stream); !slices.Equal(tags, []string{"member"}) {
		t.Errorf("tags = %v, want [member]", tags)
	}
}

func TestAProbeCountsOnlyTheStatusesTheGroupExpects(t *testing.T) {
	// The first member answers 200, which the default 2xx accepts but this group does not.
	picky, expected := newMember(t, "picky", 200), newMember(t, "expected", 204)
	statuses, err := httpstatusranges.Parse("204")
	if err != nil {
		t.Fatal(err)
	}
	groupOutbound, err := group.NewFallback(group.FallbackConfig{
		Tag:     "group",
		Members: []component.Outbound{picky, expected},
		Probe:   group.ProbeConfig{URL: "http://probe.example/", Interval: 30 * time.Millisecond, Timeout: 2 * time.Second, ExpectedStatuses: statuses},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { groupOutbound.Close() })
	groupOutbound.StartProbing(context.Background())

	eventually(t, "the member that answers 204 to be preferred", func() bool {
		return memberUsed(t, groupOutbound, picky, expected) == "expected"
	})
}

func TestAGroupReportsItsMembersAndTheOneItTriesFirst(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	groupOutbound := newTestGroup(t, false, 0, first, second)

	reporter, ok := groupOutbound.(interface {
		MemberTags() []string
		CurrentMemberTag() string
	})
	if !ok {
		t.Fatal("a groupOutbound does not report its state")
	}
	if tags := reporter.MemberTags(); len(tags) != 2 || tags[0] != "first" || tags[1] != "second" {
		t.Errorf("MemberTags = %v", tags)
	}
	if reporter.CurrentMemberTag() != "first" {
		t.Errorf("CurrentMemberTag = %q before any probe, want first", reporter.CurrentMemberTag())
	}
}
