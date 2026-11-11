package router

import (
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/kpatdev/quota-router/internal/quota"
)

func TestClockAlignedPolls(t *testing.T) {
	h := newHarness(t)
	h.roster("claude", "A", "B")
	h.advance(73 * time.Second)
	h.poll("A")
	h.poll("B")
	a := h.r.accounts["A"].NextPoll
	b := h.r.accounts["B"].NextPoll
	slot := t0.Add(5 * time.Minute)
	if a.Before(slot) || !a.Before(slot.Add(5*time.Second)) || a.Equal(b) {
		t.Fatalf("unaligned or unspread polls: %s %s", a, b)
	}
	h.advance(20 * time.Second)
	h.poll("A")
	if !h.r.accounts["A"].NextPoll.Equal(a) {
		t.Fatal("completion time shifted slot")
	}
}

func TestResetProbes(t *testing.T) {
	h := newHarness(t)
	h.roster("claude", "A")
	h.poll("A", weekly(10, 2*time.Minute))
	h.advance(90 * time.Second)
	if len(h.r.DuePolls()) != 1 {
		t.Fatal("pre-reset probe missing")
	}
	h.poll("A", weekly(10, 30*time.Second))
	if got := h.r.NextPollAt(); !got.Equal(t0.Add(150 * time.Second)) {
		t.Fatalf("post-reset probe: %s", got)
	}
	h.advance(time.Minute)
	if len(h.r.DuePolls()) != 1 {
		t.Fatal("post-reset probe missing")
	}
	h.poll("A", weekly(0, 7*24*time.Hour))
	if len(h.r.DuePolls()) != 0 {
		t.Fatal("probe repeated")
	}
}

func TestProbeSuppression(t *testing.T) {
	for _, mode := range []string{"window", "poll", "expired", "backoff"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			h.roster("claude", "A")
			h.poll("A", weekly(10, 2*time.Minute))
			a := h.r.accounts["A"]
			h.advance(90 * time.Second)
			switch mode {
			case "window":
				a.Snapshot.Windows[0].ObservedAt = h.now.Add(-10 * time.Second)
			case "poll":
				a.LastPollAt = h.now.Add(-5 * time.Second)
			case "expired":
				h.now = t0.Add(12 * time.Minute)
			case "backoff":
				h.r.ApplyPoll("A", quota.Snapshot{}, "", &quota.Error{Code: "quota_http_429", RetryAfter: 20 * time.Minute})
			}
			if got := h.r.probeAt(a, h.now); !got.IsZero() && !got.After(h.now) {
				t.Fatalf("unexpected probe: %s", got)
			}
		})
	}
}

func TestPassiveSkipAndCap(t *testing.T) {
	for _, mode := range []string{"skip", "cap", "disabled", "manual", "probe", "no_success"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t, func(c *config.Config) {
				if mode == "disabled" {
					c.PassiveSkip = 0
				}
			})
			h.roster("claude", "A")
			h.poll("A", weekly(10, time.Hour))
			a := h.r.accounts["A"]
			h.advance(5*time.Minute + 5*time.Second)
			a.PassiveAt = h.now
			if mode == "cap" {
				a.Snapshot.PolledAt = h.now.Add(-30 * time.Minute)
			}
			if mode == "no_success" {
				a.Snapshot.PolledAt = time.Time{}
			}
			if mode == "manual" {
				h.r.RequestRefresh("A")
			}
			if mode == "probe" {
				reset := h.now.Add(30 * time.Second)
				a.Snapshot.Windows[0].ResetsAt = &reset
			}
			due := h.r.DuePolls()
			if (len(due) == 0) != (mode == "skip") {
				t.Fatalf("due polls: %v", due)
			}
			if mode == "skip" && !a.NextPoll.After(h.now) {
				t.Fatal("skip did not advance slot")
			}
		})
	}
}

func TestPassiveArrivalUsesClock(t *testing.T) {
	h := newHarness(t)
	h.roster("claude", "A")
	h.poll("A", weekly(10, time.Hour))
	h.advance(5*time.Minute + 5*time.Second)
	h.r.Observe(Outcome{AuthID: "A", Provider: "claude", At: t0.Add(time.Minute), Headers: map[string][]string{
		"Anthropic-Ratelimit-Unified-5h-Status":      {"allowed"},
		"Anthropic-Ratelimit-Unified-5h-Utilization": {"0.2"},
		"Anthropic-Ratelimit-Unified-5h-Reset":       {"1791900000"},
	}})
	if !h.r.accounts["A"].PassiveAt.Equal(h.now) {
		t.Fatal("passive arrival did not use clock")
	}
	if len(h.r.DuePolls()) != 0 {
		t.Fatal("passive arrival did not skip regular poll")
	}
	h.advance(time.Minute)
	h.r.accounts["A"].NextPoll = h.now
	if len(h.r.DuePolls()) != 1 {
		t.Fatal("expired passive arrival skipped poll")
	}
}

func TestRetryAfterIsBackoffFloor(t *testing.T) {
	h := newHarness(t)
	h.roster("claude", "A")
	h.r.SetJitter(func() float64 { return 0 })
	h.r.ApplyPoll("A", quota.Snapshot{}, "", &quota.Error{Code: "quota_http_429", RetryAfter: time.Hour})
	if got := h.r.NextPollAt(); !got.Equal(h.now.Add(time.Hour)) {
		t.Fatalf("Retry-After shortened: %s", got)
	}
}

func TestProbeObservationIsPerWindow(t *testing.T) {
	h := newHarness(t)
	h.roster("claude", "A")
	h.poll("A", weekly(10, 2*time.Minute), fiveHour(20, 2*time.Minute))
	h.advance(90 * time.Second)
	h.r.accounts["A"].Snapshot.Windows[0].ObservedAt = h.now
	if len(h.r.DuePolls()) != 1 {
		t.Fatal("one observed window suppressed another window's probe")
	}
}

func TestQuota429NeverPollsFasterThanConfiguredCadence(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.PollInterval = 10 * time.Minute })
	h.roster("claude", "A", "B")
	h.r.SetJitter(func() float64 { return 0 })
	h.r.ApplyPoll("A", quota.Snapshot{}, "", &quota.Error{Code: "quota_http_429", Status: 429})
	deadline := h.r.accounts["A"].NextPoll
	if deadline.Before(h.now.Add(10 * time.Minute)) {
		t.Fatalf("429 accelerated polling: %v", deadline)
	}
	if n, wait := h.r.RequestRefresh(""); n != 1 || wait != 0 {
		t.Fatalf("healthy refresh blocked: %d %v", n, wait)
	}
	if !h.r.accounts["A"].NextPoll.Equal(deadline) {
		t.Fatal("refresh-all bypassed cooldown")
	}
	if n, wait := h.r.RequestRefresh("A"); n != 0 || wait < 10*time.Minute {
		t.Fatalf("single refresh bypassed cooldown: %d %v", n, wait)
	}
	for _, task := range h.r.DuePolls() {
		if task.AuthID == "A" {
			t.Fatal("rate-limited account polled")
		}
	}
}

func TestQuotaCooldownSurvivesStateReload(t *testing.T) {
	h := newHarness(t)
	h.roster("claude", "A")
	h.r.ApplyPoll("A", quota.Snapshot{}, "", &quota.Error{Code: "quota_http_429", RetryAfter: 20 * time.Minute})
	state := h.r.Export()
	restored := New(h.r.Config(), func() time.Time { return h.now })
	restored.Import(state)
	if !restored.accounts["A"].NextPoll.Equal(h.r.accounts["A"].NextPoll) {
		t.Fatal("reload erased cooldown")
	}
	if len(restored.DuePolls()) != 0 {
		t.Fatal("reload polled rate-limited account")
	}
}
