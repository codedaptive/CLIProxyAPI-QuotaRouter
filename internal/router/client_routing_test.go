package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/kpatdev/quota-router/internal/quota"
	"github.com/kpatdev/quota-router/internal/session"
)

const desktopKey = "synthetic-desktop-key"

func desktopHarness(t *testing.T, provider string) *harness {
	h := newHarness(t, func(c *config.Config) {
		c.ClientRouting.Enabled = true
		c.ClientRouting.DesktopCallerScopes = []string{session.CallerScope(desktopKey)}
		c.ClientRouting.AccountPlans = map[string]string{"large": "pro", "small": "plus"}
		if provider == "claude" {
			c.ClientRouting.AccountPlans = map[string]string{"large": "max_20x", "small": "pro"}
		}
	})
	h.roster(provider, "large", "small")
	h.poll("large", weekly(20, 6*24*time.Hour), fiveHour(20, time.Hour))
	h.poll("small", weekly(40, 24*time.Hour), fiveHour(10, time.Hour))
	return h
}

func policyRequest(provider, key, sid string) PickRequest {
	h := codexSession(sid)
	if provider == "claude" {
		h = claudeSession(sid)
	}
	ref, ok := session.FromRequest(h, map[string]any{"caller_scope": session.CallerScope(key)})
	return PickRequest{Provider: provider, Model: provider + "-model", Session: ref, HasSession: ok, Candidates: []Candidate{{ID: "large", Provider: provider, Priority: 1}, {ID: "small", Provider: provider, Priority: 2}}}
}

func TestDesktopPreferenceCLIPriorityAndNoLearnedCLIBindings(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			h := desktopHarness(t, provider)
			desktop := policyRequest(provider, desktopKey, "chat")
			cli := policyRequest(provider, "synthetic-cli-key", "chat")
			expect(t, h.r.Pick(desktop), OutcomeNewSession, "large")
			expect(t, h.r.Pick(cli), OutcomeNoSession, "small")
			h.r.Observe(Outcome{At: h.now, AuthID: "small", Provider: provider, Model: cli.Model, Session: cli.Session, HasSession: true, Status: 200})
			if len(h.r.Export().Bindings) != 1 {
				t.Fatal("CLI usage created a binding")
			}
			// A quota/priority change affects CLI but not the desktop conversation.
			cli.Candidates[0].Priority = 3
			expect(t, h.r.Pick(cli), OutcomeNoSession, "large")
			expect(t, h.r.Pick(desktop), OutcomeAffinity, "large")
			// A different model within the same provider retains the desktop account.
			desktop.Model = "another-model"
			expect(t, h.r.Pick(desktop), OutcomeAffinity, "large")
		})
	}
}

func TestDesktopQuotaRebindDoesNotSnapBack(t *testing.T) {
	for _, kind := range []quota.Kind{quota.FiveHour, quota.Weekly} {
		t.Run(string(kind), func(t *testing.T) {
			h := desktopHarness(t, "codex")
			req := policyRequest("codex", desktopKey, "chat")
			expect(t, h.r.Pick(req), OutcomeNewSession, "large")
			if kind == quota.FiveHour {
				h.poll("large", weekly(20, 6*24*time.Hour), fiveHour(100, time.Hour))
			} else {
				h.poll("large", weekly(100, 6*24*time.Hour), fiveHour(20, time.Hour))
			}
			expect(t, h.r.Pick(req), OutcomeRebind, "small")
			h.poll("large", weekly(20, 6*24*time.Hour), fiveHour(0, time.Hour))
			expect(t, h.r.Pick(req), OutcomeAffinity, "small")
			fresh := policyRequest("codex", desktopKey, "new-chat")
			expect(t, h.r.Pick(fresh), OutcomeNewSession, "large")
		})
	}
}

func TestDesktopTemporaryFailureDoesNotRebind(t *testing.T) {
	h := desktopHarness(t, "claude")
	req := policyRequest("claude", desktopKey, "chat")
	expect(t, h.r.Pick(req), OutcomeNewSession, "large")
	h.r.Observe(Outcome{At: h.now, AuthID: "large", Provider: "claude", Model: req.Model, Session: req.Session, HasSession: true, Failed: true, Status: 429, Headers: http.Header{"Retry-After": {"60"}}})
	expect(t, h.r.Pick(req), OutcomeFailover, "small")
	h.advance(2 * time.Minute)
	expect(t, h.r.Pick(req), OutcomeAffinity, "large")
}

func TestDesktopBindingsSurviveIdleRestartAndInherit(t *testing.T) {
	h := desktopHarness(t, "codex")
	req := policyRequest("codex", desktopKey, "parent")
	expect(t, h.r.Pick(req), OutcomeNewSession, "large")
	h.advance(8 * 24 * time.Hour)
	h.r.Sweep()
	raw, err := json.Marshal(h.r.Export())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), desktopKey) || strings.Contains(string(raw), "codex:parent") {
		t.Fatal("raw session or credential persisted")
	}
	var state State
	if err = json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	restored := New(h.r.Config(), func() time.Time { return h.now })
	restored.Import(state)
	h.r = restored
	h.roster("codex", "large", "small")
	h.poll("large", weekly(20, 24*time.Hour), fiveHour(10, time.Hour))
	h.poll("small", weekly(10, 12*time.Hour), fiveHour(0, time.Hour))
	expect(t, h.r.Pick(req), OutcomeAffinity, "large")
	child := req
	child.Session, child.HasSession = session.FromRequest(codexChild("parent", "child"), map[string]any{"caller_scope": session.CallerScope(desktopKey)})
	expect(t, h.r.Pick(child), OutcomeInherited, "large")
}

func TestDesktopMissingIdentityAndUntrustedClientHeaders(t *testing.T) {
	h := desktopHarness(t, "codex")
	ref, ok := session.FromRequest(nil, map[string]any{"caller_scope": session.CallerScope(desktopKey)})
	req := policyRequest("codex", desktopKey, "chat")
	req.Session = ref
	req.HasSession = ok
	d := h.r.Pick(req)
	expect(t, d, OutcomeNoSession, "large")
	if d.Cause != "desktop_session_missing" || len(h.r.Export().Bindings) != 0 {
		t.Fatal("invented affinity for sessionless desktop")
	}
	ref, ok = session.FromRequest(http.Header{"Session-Id": {"chat"}, "User-Agent": {"CodexDesktop"}, "X-Client-Type": {"desktop"}}, nil)
	req.Session = ref
	req.HasSession = ok
	expect(t, h.r.Pick(req), OutcomeNoSession, "small")
}

func TestCLILegacyResetAndIDTiesIgnoreCapacityAndModelReset(t *testing.T) {
	h := desktopHarness(t, "claude")
	req := policyRequest("claude", "cli", "chat")
	req.Candidates[0].Priority = 2
	h.poll("large", weekly(99, 24*time.Hour), fiveHour(10, time.Hour))
	h.poll("small", weekly(0, 24*time.Hour), fiveHour(0, time.Hour))
	expect(t, h.r.Pick(req), OutcomeNoSession, "large") // ID tie, not capacity/headroom.
	h.poll("small", weekly(0, 12*time.Hour), fiveHour(0, time.Hour))
	expect(t, h.r.Pick(req), OutcomeNoSession, "small")
	if len(h.r.Export().Bindings) != 0 {
		t.Fatal("CLI must not bind")
	}
}

func TestDesktopPreferenceUsesReportedPlanWithoutOverride(t *testing.T) {
	h := desktopHarness(t, "codex")
	cfg := h.r.Config()
	cfg.ClientRouting.AccountPlans = nil
	h.r.Configure(cfg)
	h.r.accounts["large"].PlanType = "pro"
	h.r.accounts["small"].PlanType = "plus"
	expect(t, h.r.Pick(policyRequest("codex", desktopKey, "chat")), OutcomeNewSession, "large")
}

func TestDesktopCallerIsolationAndReadOnlyPreviews(t *testing.T) {
	h := desktopHarness(t, "codex")
	cfg := h.r.Config()
	cfg.ClientRouting.DesktopCallerScopes = append(cfg.ClientRouting.DesktopCallerScopes, session.CallerScope("other-desktop"))
	h.r.Configure(cfg)
	first := policyRequest("codex", desktopKey, "same-chat-id")
	expect(t, h.r.Pick(first), OutcomeNewSession, "large")
	h.poll("large", weekly(100, 6*24*time.Hour))
	expect(t, h.r.Pick(first), OutcomeRebind, "small")
	h.poll("large", weekly(10, 6*24*time.Hour))
	second := policyRequest("codex", "other-desktop", "same-chat-id")
	expect(t, h.r.Pick(second), OutcomeNewSession, "large")
	expect(t, h.r.Pick(first), OutcomeAffinity, "small")
	before, _ := json.Marshal(h.r.Export())
	previews := h.r.NextUp()
	if len(previews) != 4 {
		t.Fatalf("expected two policies per provider: %+v", previews)
	}
	for _, p := range previews {
		if p.Provider != "codex" {
			continue
		}
		want := "small"
		if p.Policy == "desktop_preferred_plan_affinity" {
			want = "large"
		}
		if p.AuthID != want {
			t.Errorf("preview %s: got %s, want %s", p.Policy, p.AuthID, want)
		}
	}
	for _, s := range h.r.Sessions() {
		if s.ExpiresAt != nil {
			t.Fatal("desktop session incorrectly advertises an idle expiry")
		}
	}
	after, _ := json.Marshal(h.r.Export())
	if string(before) != string(after) {
		t.Fatal("reading previews mutated routing state")
	}
}

func TestDesktopUltraPreferenceMatchesLegacyMax(t *testing.T) {
	for _, preferred := range []string{"max", "ultra"} {
		h := desktopHarness(t, "claude")
		h.r.cfg.ClientRouting.PreferredPlans["claude"] = []string{preferred}
		h.r.cfg.ClientRouting.AccountPlans["large"] = "ultra"
		req := policyRequest("claude", desktopKey, "ultra-chat")
		if d := h.r.Pick(req); d.AuthID != "large" {
			t.Fatalf("preference=%s: %+v", preferred, d)
		}
	}
}
