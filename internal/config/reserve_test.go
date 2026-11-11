package config

import (
	"testing"
	"time"
)

func TestMaxReserveDefaultsValidationAndPatch(t *testing.T) {
	c, err := Decode([]byte("max_weekly_reserve: {enabled: true}"))
	if err != nil || !c.MaxWeeklyReserve.Enabled || c.MaxWeeklyReserve.RemainingPercent != 10 || c.MaxWeeklyReserve.ReleaseBefore != 24*time.Hour {
		t.Fatalf("%+v %v", c.MaxWeeklyReserve, err)
	}
	next, err := DecodeJSONPatch(c, []byte(`{"strategy":"balanced"}`))
	if err != nil || next.MaxWeeklyReserve != c.MaxWeeklyReserve {
		t.Fatalf("patch lost reserve: %v", err)
	}
	if Default().MaxWeeklyReserve.Enabled {
		t.Fatal("must be opt in")
	}
	for _, raw := range []string{
		"max_weekly_reserve: {remaining_percent: -1}",
		"max_weekly_reserve: {remaining_percent: 100}",
		"max_weekly_reserve: {remaining_percent: .nan}",
		"max_weekly_reserve: {release_before: 0s}",
		"max_weekly_reserve: {release_before: 169h}",
		"max_weekly_reserve: {enabled: true}\nschedule_across_priorities: false",
	} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
