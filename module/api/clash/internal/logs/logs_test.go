package logs_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/logs"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return mountBuffered(t, provider, 256)
}

func mountBuffered(t *testing.T, provider *testkit.Provider, bufferRecords int) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, hub *stream.Hub) { logs.Register(router, provider, hub, bufferRecords) })
}

func recordAt(level slog.Level, message string, attributes ...slog.Attr) slog.Record {
	record := slog.NewRecord(time.Now(), level, message, 0)
	record.AddAttrs(attributes...)
	return record
}

type subscribed struct {
	level    slog.Level
	buffered int
}

func logsProvider(subscription *testkit.LogSubscription, into *subscribed) *testkit.Provider {
	return &testkit.Provider{SubscribeFunc: func(level slog.Level, buffered int) (spec.APILogSubscription, error) {
		if into != nil {
			*into = subscribed{level, buffered}
		}
		return subscription, nil
	}}
}

func TestLogsStreamsRecordsAsClashLines(t *testing.T) {
	subscription := testkit.NewLogSubscription()
	var asked subscribed
	server := mount(t, logsProvider(subscription, &asked))

	_, reader := server.OpenLines("/logs")
	subscription.Feed <- recordAt(slog.LevelInfo, "Inbound active", slog.String("tag", "in"), slog.Int("port", 1080))
	subscription.Feed <- recordAt(slog.LevelWarn, "Slow", slog.String("reason", "a b"), slog.Group("conn", slog.String("id", "7")))
	subscription.Feed <- recordAt(slog.Level(10), "Critical thing")
	subscription.Feed <- recordAt(slog.LevelDebug, "Details")

	want := []logLineWant{
		{"info", "Inbound active tag=in port=1080"},
		{"warning", `Slow reason="a b" conn.id=7`},
		{"error", "Critical thing"},
		{"debug", "Details"},
	}
	for _, expected := range want {
		var line logLineWant
		if err := json.Unmarshal([]byte(testkit.ReadLine(t, reader)), &line); err != nil || line != expected {
			t.Errorf("line %+v (%v), want %+v", line, err, expected)
		}
	}
	if asked.level != slog.LevelInfo || asked.buffered != 256 {
		t.Errorf("subscribed %+v, want info and the default buffer of 256", asked)
	}
}

type logLineWant struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

func TestLogsLevelComesFromTheQueryAndTheBufferFromTheSettings(t *testing.T) {
	for query, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo, "warning": slog.LevelWarn, "error": slog.LevelError, "silent": slog.Level(1000),
	} {
		subscription := testkit.NewLogSubscription()
		var asked subscribed
		server := mountBuffered(t, logsProvider(subscription, &asked), 64)
		server.OpenLines("/logs?level=" + query)
		testkit.Eventually(t, "the subscription", func() bool { return asked.buffered != 0 })
		if asked.level != want || asked.buffered != 64 {
			t.Errorf("level %q: subscribed %+v, want level %d and buffer 64", query, asked, want)
		}
	}
}

func TestLogsRefusesAnUnknownLevel(t *testing.T) {
	server := mount(t, logsProvider(testkit.NewLogSubscription(), nil))
	status, body, _ := server.Do("GET", "/logs?level=loud", "")
	if status != http.StatusBadRequest || !strings.Contains(testkit.MessageOf(t, body), "loud") {
		t.Errorf("%d %q", status, body)
	}
}

func TestLogsCancelsItsSubscriptionWhenTheClientLeaves(t *testing.T) {
	subscription := testkit.NewLogSubscription()
	server := mount(t, logsProvider(subscription, nil))
	response, reader := server.OpenLines("/logs")
	subscription.Feed <- recordAt(slog.LevelInfo, "one")
	testkit.ReadLine(t, reader)

	response.Body.Close()
	select {
	case <-subscription.Cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription was never cancelled")
	}
}

func TestLogsEndsWhenTheCoreEndsTheSubscription(t *testing.T) {
	subscription := testkit.NewLogSubscription()
	server := mount(t, logsProvider(subscription, nil))
	_, reader := server.OpenLines("/logs")
	close(subscription.Feed)
	if _, err := reader.ReadString('\n'); err == nil {
		t.Error("the stream stayed open after the subscription closed")
	}
}

func TestLogsOverAWebSocketAndItsCancellationWhenTheServerCloses(t *testing.T) {
	subscription := testkit.NewLogSubscription()
	server := mount(t, logsProvider(subscription, nil))
	socket, err := server.DialWebSocket("/logs?level=info", "")
	if err != nil {
		t.Fatalf("the upgrade was refused: %v", err)
	}
	subscription.Feed <- recordAt(slog.LevelInfo, "over ws")
	var line logLineWant
	if err := json.Unmarshal([]byte(testkit.ReadText(t, socket)), &line); err != nil || line.Payload != "over ws" {
		t.Errorf("line %+v, %v", line, err)
	}

	server.Hub.Close()
	select {
	case <-subscription.Cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the server left the subscription open")
	}
}

func TestLogsForwardsTheNoticeAboutDroppedRecordsAsAnyOtherRecord(t *testing.T) {
	subscription := testkit.NewLogSubscription()
	server := mount(t, logsProvider(subscription, nil))
	_, reader := server.OpenLines("/logs")
	subscription.Feed <- recordAt(slog.LevelWarn, "37 log records were dropped", slog.Int("dropped_records", 37))
	var line logLineWant
	json.Unmarshal([]byte(testkit.ReadLine(t, reader)), &line)
	if line.Type != "warning" || !strings.Contains(line.Payload, "dropped_records=37") {
		t.Errorf("line %+v", line)
	}
}

func TestLogsReportsAFailedSubscriptionBeforeStreaming(t *testing.T) {
	provider := &testkit.Provider{SubscribeFunc: func(slog.Level, int) (spec.APILogSubscription, error) { return nil, errors.New("closed") }}
	status, _, _ := mount(t, provider).Do("GET", "/logs", "")
	if status != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", status)
	}
}
