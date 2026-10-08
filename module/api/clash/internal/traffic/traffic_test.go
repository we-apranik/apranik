package traffic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/traffic"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, hub *stream.Hub) { traffic.Register(router, provider, hub) })
}

type message struct{ Up, Down, UpTotal, DownTotal uint64 }

func growing(uplinkStep, downlinkStep uint64) func() (spec.APITrafficTotal, error) {
	var uplink, downlink atomic.Uint64
	return func() (spec.APITrafficTotal, error) {
		return spec.APITrafficTotal{UplinkBytes: uplink.Add(uplinkStep), DownlinkBytes: downlink.Add(downlinkStep)}, nil
	}
}

func TestTrafficSendsTheBytesOfEachTickAsJSONLines(t *testing.T) {
	env := mount(t, &testkit.Provider{TrafficFunc: growing(100, 250)})

	response, reader := env.OpenLines("/traffic")
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	var first, second message
	if err := json.Unmarshal([]byte(testkit.ReadLine(t, reader)), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(testkit.ReadLine(t, reader)), &second); err != nil {
		t.Fatal(err)
	}
	if first.Up != 100 || first.Down != 250 || second.Up != 100 || second.Down != 250 || second.UpTotal != first.UpTotal+100 || second.DownTotal != first.DownTotal+250 {
		t.Errorf("first %+v, second %+v; want a difference of 100 up and 250 down each time and rising totals", first, second)
	}
}

func TestTrafficOverAWebSocket(t *testing.T) {
	env := mount(t, &testkit.Provider{TrafficFunc: growing(7, 0)})
	connection, err := env.DialWebSocket("/traffic", "")
	if err != nil {
		t.Fatalf("the upgrade was refused: %v", err)
	}
	var got message
	if err := json.Unmarshal([]byte(testkit.ReadText(t, connection)), &got); err != nil || got.UpTotal < 14 || got.Up != 7 {
		t.Errorf("message %+v, %v", got, err)
	}
}

func TestACounterThatWentDownIsAZeroDifferenceNotAHugeOne(t *testing.T) {
	var calls atomic.Int32
	env := mount(t, &testkit.Provider{TrafficFunc: func() (spec.APITrafficTotal, error) {
		if calls.Add(1) == 1 {
			return spec.APITrafficTotal{UplinkBytes: 1000, DownlinkBytes: 1000}, nil
		}
		return spec.APITrafficTotal{UplinkBytes: 10, DownlinkBytes: 10}, nil
	}})
	_, reader := env.OpenLines("/traffic")
	var got message
	if err := json.Unmarshal([]byte(testkit.ReadLine(t, reader)), &got); err != nil || got.Up != 0 || got.Down != 0 || got.UpTotal != 10 {
		t.Errorf("message %+v, %v", got, err)
	}
}

func TestTrafficWithStatisticsOffIsA503BeforeAnyStreamStarts(t *testing.T) {
	env := mount(t, &testkit.Provider{TrafficFunc: func() (spec.APITrafficTotal, error) { return spec.APITrafficTotal{}, spec.ErrAPIStatisticsDisabled }})
	status, body, _ := env.Do("GET", "/traffic", "")
	if status != http.StatusServiceUnavailable || !strings.Contains(testkit.MessageOf(t, body), "statistics are disabled") {
		t.Errorf("status %d body %q", status, body)
	}
}

func TestTheStreamEndsWhenTheProviderStopsAnswering(t *testing.T) {
	var calls atomic.Int32
	env := mount(t, &testkit.Provider{TrafficFunc: func() (spec.APITrafficTotal, error) {
		if calls.Add(1) > 2 {
			return spec.APITrafficTotal{}, context.Canceled
		}
		return spec.APITrafficTotal{}, nil
	}})
	_, reader := env.OpenLines("/traffic")
	testkit.ReadLine(t, reader)
	if _, err := reader.ReadString('\n'); err == nil {
		t.Error("the stream stayed open after the provider failed")
	}
}
