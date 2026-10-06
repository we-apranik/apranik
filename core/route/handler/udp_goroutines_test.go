package handler_test

import (
	"runtime"
	"testing"

	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/core/stats"
)

// A UDP session must not cost goroutines beyond the one running HandleDatagram,
// the relay's two and the session's reply loop per outbound: the idle sweeps
// share one goroutine. Before they did, each session held two more.
func TestManyUDPSessionsShareOneSweeperGoroutine(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry})

	before := runtime.NumGoroutine()
	const sessions = 200
	for range sessions {
		session := startUDPSession(t, trafficHandler, udpMetadata("alice"), "abcd")
		session.exchange(t, udpTarget(t, "192.0.2.1", 53))
	}
	perSession := float64(runtime.NumGoroutine()-before) / sessions
	t.Logf("%.2f goroutines per UDP session (the one running the handler, two in the relay, one reading replies; the sweeper is one for all)", perSession)
	if perSession > 4.1 {
		t.Errorf("%.2f goroutines per UDP session, want at most 4: the idle sweeps must not add one each", perSession)
	}
}
