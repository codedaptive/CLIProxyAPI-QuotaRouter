package router

import (
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/kpatdev/quota-router/internal/quota"
)

func reserveHarness(t *testing.T) *harness {
	h := newHarness(t, func(c *config.Config) {
		c.MaxWeeklyReserve.Enabled = true
		c.NewSessionMinRemaining = 0
		c.ClientRouting.AccountPlans = map[string]string{"max": "max_20x"}
	})
	h.roster("claude", "max", "team")
	h.poll("max", weekly(90, 48*time.Hour))
	h.poll("team", weekly(95, 96*time.Hour))
	return h
}
func TestWeeklyReserveThresholdAndReleaseBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		used  float64
		reset time.Duration
		want  string
	}{
		{"above floor", 89.9, 48 * time.Hour, "max"},
		{"at floor", 90, 48 * time.Hour, "team"},
		{"below floor", 99, 48 * time.Hour, "team"},
		{"just outside release", 90, 24*time.Hour + time.Nanosecond, "team"},
		{"exact release", 90, 24 * time.Hour, "max"},
		{"inside release", 99, time.Hour, "max"},
		{"exhausted even within release", 100, time.Hour, "team"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := reserveHarness(t)
			h.poll("max", weekly(tc.used, tc.reset))
			d := h.pick("claude", "opus", nil, "max", "team")
			if d.AuthID != tc.want {
				t.Fatalf("%+v", d)
			}
		})
	}
}
func TestWeeklyReserveOverridesAffinityAndInheritedSession(t *testing.T) {
	h := reserveHarness(t)
	h.poll("max", weekly(89, 48*time.Hour))
	parent := claudeSession("parent")
	if d := h.pick("claude", "opus", parent, "max", "team"); d.AuthID != "max" {
		t.Fatalf("%+v", d)
	}
	h.poll("max", weekly(90, 48*time.Hour))
	d := h.pick("claude", "opus", parent, "max", "team")
	if d.AuthID != "team" || d.Outcome != OutcomeFailover || d.Cause != "weekly_reserve_protected" {
		t.Fatalf("%+v", d)
	}
	if d = h.pick("claude", "opus", claudeAgent("parent", "child"), "max", "team"); d.AuthID != "team" {
		t.Fatalf("inherited reserve: %+v", d)
	}
	h.poll("max", weekly(95, 24*time.Hour))
	if d = h.pick("claude", "opus", parent, "max", "team"); d.AuthID != "max" {
		t.Fatalf("release: %+v", d)
	}
}
func TestWeeklyReserveNeverDelegatesProtectedPoolOrHostPin(t *testing.T) {
	h := reserveHarness(t)
	for _, req := range []PickRequest{
		{Provider: "claude", Candidates: []Candidate{{ID: "max", Provider: "claude"}}},
		{Provider: "claude", PinnedAuthID: "max", Candidates: []Candidate{{ID: "max", Provider: "claude"}, {ID: "team", Provider: "claude"}}},
		{Provider: "mixed", Candidates: []Candidate{{ID: "max", Provider: "claude"}}},
	} {
		d := h.r.Pick(req)
		if !d.Applied || !d.Reject || d.AuthID != "" {
			t.Fatalf("reserve bypass: %+v", d)
		}
	}
	if h.r.RoleEligible("max", "claude", "opus") {
		t.Fatal("role bypassed reserve")
	}
	h.r.cfg.Mode = config.ModeShadow
	d := h.pick("claude", "opus", nil, "max")
	if d.Applied || !d.Reject || h.r.ReserveBlocked("max", "claude") {
		t.Fatalf("shadow must not enforce: %+v", d)
	}
}
func TestWeeklyReserveUnknownStaleAndExpiredDoNotSpend(t *testing.T) {
	for _, kind := range []string{"missing", "stale", "expired", "missing reset", "missing window"} {
		t.Run(kind, func(t *testing.T) {
			h := reserveHarness(t)
			switch kind {
			case "missing":
				h.r.accounts["max"].HasSnapshot = false
			case "stale":
				h.advance(h.r.cfg.MaxAge + time.Second)
			case "expired":
				h.poll("max", weekly(10, -time.Second))
			case "missing reset":
				h.r.accounts["max"].Snapshot.Windows[0].ResetsAt = nil
			case "missing window":
				h.poll("max", fiveHour(0, time.Hour))
			}
			d := h.pick("claude", "opus", nil, "max")
			if !d.Reject || d.AuthID != "" {
				t.Fatalf("%+v", d)
			}
		})
	}
}
func TestWeeklyReserveOnlyClaudeMaxAccountWideWeekly(t *testing.T) {
	h := reserveHarness(t)
	h.r.cfg.MaxWeeklyReserve.Enabled = false
	if d := h.pick("claude", "opus", nil, "max", "team"); d.AuthID != "max" {
		t.Fatalf("disabled: %+v", d)
	}
	h.r.cfg.MaxWeeklyReserve.Enabled = true
	h.r.cfg.ClientRouting.AccountPlans["max"] = "pro"
	if d := h.pick("claude", "opus", nil, "max", "team"); d.AuthID != "max" {
		t.Fatalf("Pro changed: %+v", d)
	}
	h.r.cfg.ClientRouting.AccountPlans["max"] = "max"
	h.poll("max", weekly(20, 48*time.Hour), fiveHour(95, time.Hour), win{quota.ModelWeekly, "opus", 95, 48 * time.Hour})
	if d := h.pick("claude", "opus", nil, "max", "team"); d.AuthID != "max" {
		t.Fatalf("wrong window: %+v", d)
	}
	h.roster("codex", "max")
	h.poll("max", weekly(99, 48*time.Hour))
	if h.r.ReserveBlocked("max", "codex") {
		t.Fatal("Codex changed")
	}
}
func TestWeeklyReserveCLIPriorityAndDesktopPreference(t *testing.T) {
	for _, desktop := range []bool{false, true} {
		h := desktopHarness(t, "claude")
		h.r.cfg.MaxWeeklyReserve.Enabled = true
		h.poll("large", weekly(90, 48*time.Hour))
		key := "cli-key"
		if desktop {
			key = desktopKey
		}
		req := policyRequest("claude", key, "chat")
		req.Candidates[0].Priority = 10
		if d := h.r.Pick(req); d.AuthID != "small" {
			t.Fatalf("priority or plan bypass: %+v", d)
		}
	}
}

func TestWeeklyReserveMixedPoolUsesSafeHostCandidate(t *testing.T) {
	h := reserveHarness(t)
	req := PickRequest{Provider: "mixed", Candidates: []Candidate{
		{ID: "max", Provider: "claude", Priority: 100},
		{ID: "api", Provider: "other", Priority: 2},
		{ID: "team", Provider: "claude", Priority: 1},
	}}
	d := h.r.Pick(req)
	if d.Reject || d.AuthID != "api" || !d.Applied {
		t.Fatalf("safe mixed fallback: %+v", d)
	}
	req.PinnedAuthID = "max"
	if d = h.r.Pick(req); !d.Reject || d.AuthID != "" {
		t.Fatalf("mixed pin bypass: %+v", d)
	}
}

func TestUltraReserveFromPlanMetadataAndOverride(t *testing.T) {
	for _, plan := range []string{"ultra", "claude_ultra", "ultra_20x", "max", "max_20x"} {
		for _, override := range []bool{false, true} {
			h := reserveHarness(t)
			if override {
				h.r.cfg.ClientRouting.AccountPlans["max"] = plan
			} else {
				delete(h.r.cfg.ClientRouting.AccountPlans, "max")
				h.r.accounts["max"].PlanType = plan
			}
			if d := h.pick("claude", "opus", nil, "max", "team"); d.AuthID != "team" {
				t.Fatalf("plan=%s override=%t bypassed reserve: %+v", plan, override, d)
			}
			if h.r.RoleEligible("max", "claude", "opus") {
				t.Fatalf("plan=%s role bypass", plan)
			}
			h.poll("max", weekly(95, 24*time.Hour))
			if h.r.ReserveBlocked("max", "claude") {
				t.Fatalf("plan=%s reserve not released", plan)
			}
		}
	}
}
