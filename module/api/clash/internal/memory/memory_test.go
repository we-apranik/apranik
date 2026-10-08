package memory_test

import (
	"encoding/json"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/memory"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
)

func mount(t *testing.T) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, hub *stream.Hub) { memory.Register(router, hub) })
}

func TestMemoryReportsWhatTheProcessHoldsAndNoLimit(t *testing.T) {
	_, reader := mount(t).OpenLines("/memory")
	var got struct{ InUse, OSLimit uint64 }
	if err := json.Unmarshal([]byte(testkit.ReadLine(t, reader)), &got); err != nil || got.InUse == 0 || got.OSLimit != 0 {
		t.Errorf("message %+v, %v; want a memory in use and no limit", got, err)
	}
}

func TestMemoryOverAWebSocket(t *testing.T) {
	connection, err := mount(t).DialWebSocket("/memory", "")
	if err != nil {
		t.Fatalf("the upgrade was refused: %v", err)
	}
	var got struct{ InUse uint64 }
	if err := json.Unmarshal([]byte(testkit.ReadText(t, connection)), &got); err != nil || got.InUse == 0 {
		t.Errorf("message %+v, %v", got, err)
	}
}

func TestInUseIsWhatTheProcessHoldsFromTheSystem(t *testing.T) {
	if memory.InUse() == 0 {
		t.Error("InUse = 0")
	}
}
