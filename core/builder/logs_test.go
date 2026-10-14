package builder_test

import (
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/builder"
	"github.com/we-apranik/apranik/core/logging"
)

func TestALoggerHandedOutBeforeSubscribingReachesALaterSubscriber(t *testing.T) {
	stackBuilder, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	defer stackBuilder.Close()
	componentLogger := stackBuilder.Logger().With("outbound", "auto")

	subscription, err := stackBuilder.SubscribeToLog(logging.LevelDebug, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()
	componentLogger.Debug("Group member chosen", "member", "server-a")

	select {
	case record := <-subscription.Records():
		if record.Message != "Group member chosen" {
			t.Errorf("record = %q", record.Message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the subscriber received nothing")
	}
}
