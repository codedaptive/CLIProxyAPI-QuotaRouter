package router

import (
	"math"
	"sort"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/kpatdev/quota-router/internal/quota"
)

// weeklyReserveCause runs under r.mu. Missing/stale weekly evidence cannot
// release a known Max account's reserve. Other plans/providers are unchanged.
func (r *Router) weeklyReserveCause(id, provider string, now time.Time) string {
	policy := r.cfg.MaxWeeklyReserve
	if !policy.Enabled || normalizeProvider(provider) != "claude" {
		return ""
	}
	plan := r.cfg.ClientRouting.AccountPlans[id]
	a := r.accounts[id]
	if plan == "" && a != nil {
		plan = a.PlanType
	}
	if config.NormalizePlan(plan) != "max" {
		return ""
	}
	if a == nil || !a.HasSnapshot {
		return "weekly_reserve_quota_unknown"
	}
	w, ok := a.Snapshot.Window(string(quota.Weekly))
	if !ok || w.ResetsAt == nil || w.ObservedAt.IsZero() || w.ObservedAt.After(now) ||
		now.Sub(w.ObservedAt) > r.cfg.MaxAge || math.IsNaN(w.UsedPercent) ||
		math.IsInf(w.UsedPercent, 0) || w.UsedPercent < 0 || w.UsedPercent > 100 {
		return "weekly_reserve_quota_unknown"
	}
	if !w.ResetsAt.After(now) {
		return "weekly_reserve_awaiting_reset"
	}
	if w.ResetsAt.Sub(now) > policy.ReleaseBefore && w.RemainingPercent() <= policy.RemainingPercent {
		return "weekly_reserve_protected"
	}
	return ""
}

// ReserveBlocked reports strict protection without a request candidate pool.
// Execution uses ReserveAllowed to apply the last-available exception.
func (r *Router) ReserveBlocked(id, provider string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Mode == config.ModeActive && r.weeklyReserveCause(id, provider, r.now()) != ""
}

// The ABI cannot delegate a filtered mixed-provider pool. When protection is
// needed, choose a safe host-offered candidate deterministically instead.
func (r *Router) pickReserveFallback(req PickRequest, d Decision, order []*eval) Decision {
	var usable []Candidate
	for _, e := range order {
		if e.usable {
			usable = append(usable, e.c)
		}
	}
	sort.Slice(usable, func(i, j int) bool {
		if usable[i].Priority != usable[j].Priority {
			return usable[i].Priority > usable[j].Priority
		}
		return usable[i].ID < usable[j].ID
	})
	for _, c := range usable {
		if req.PinnedAuthID != "" && c.ID != req.PinnedAuthID {
			continue
		}
		d.AuthID, d.Outcome, d.Cause = c.ID, OutcomeFailover, "weekly_reserve_safe_fallback"
		return r.finish(d)
	}
	return r.finish(d)
}

// evaluatePool releases only the percentage reserve, and only when no ordinary
// host-offered candidate is usable for this model. Unknown quota on another
// account still gets its normal opportunity; missing/stale reserve evidence never
// becomes permission to spend. Runs under r.mu; policy is local to this request.
func (r *Router) evaluatePool(candidates []Candidate, model string, now time.Time) ([]*eval, map[string]bool) {
	var order []*eval
	hasAlternative := false
	for _, c := range candidates {
		if c.ID == "" {
			continue
		}
		e := r.evaluate(c, model, now)
		order = append(order, e)
		hasAlternative = hasAlternative || e.usable
	}
	released := map[string]bool{}
	if hasAlternative {
		return order, released
	}
	for i, e := range order {
		if e.cause != "weekly_reserve_protected" {
			continue
		}
		fallback := r.evaluateReserve(e.c, model, now, true)
		if fallback.usable && fallback.assessment.State == quota.Ready {
			order[i] = fallback
			released[e.c.ID] = true
		}
	}
	return order, released
}

// ReserveAllowed evaluates the same candidate pool for role callbacks as
// ordinary picks. The host has already filtered this pool for model/access scope.
func (r *Router) ReserveAllowed(req PickRequest) map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]bool{}
	order, released := r.evaluatePool(req.Candidates, req.Model, r.now())
	for _, e := range order {
		out[e.c.ID] = r.cfg.Mode != config.ModeActive ||
			r.weeklyReserveCause(e.c.ID, e.c.Provider, r.now()) == "" || released[e.c.ID]
	}
	return out
}
