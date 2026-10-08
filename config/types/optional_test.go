package types

import (
	"testing"
	"time"
)

func TestAWrittenValueWinsAndAnAbsentOneIsTheDefault(t *testing.T) {
	if got := ValueOrDefault(Pointer(Duration(3*time.Second)), Duration(time.Second)); got != Duration(3*time.Second) {
		t.Errorf("written = %s, want 3s", got)
	}
	if got := ValueOrDefault((*int)(nil), 7); got != 7 {
		t.Errorf("absent = %d, want the default 7", got)
	}
	if got := ValueOrDefault(Pointer(0), 7); got != 0 {
		t.Errorf("a written zero = %d, want 0: zero is not the default", got)
	}
}

func TestOnlyAPositiveOrAbsentSettingPasses(t *testing.T) {
	if CheckPositive((*Duration)(nil)) != nil || CheckPositive(Pointer(Duration(time.Second))) != nil || CheckPositive(Pointer(5)) != nil {
		t.Error("an absent or positive setting was refused")
	}
	if CheckPositive(Pointer(Duration(0))) == nil || CheckPositive(Pointer(Duration(-time.Second))) == nil || CheckPositive(Pointer(0)) == nil || CheckPositive(Pointer(-1)) == nil {
		t.Error("a written zero or negative setting passed")
	}
}
