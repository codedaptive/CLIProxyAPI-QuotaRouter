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

// ReserveBlocked is also checked at the role executor's exact-account pin,
// closing the gap between initial role selection and the later host callback.
func (r *Router) ReserveBlocked(id, provider string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Mode == config.ModeActive && r.weeklyReserveCause(id, provider, r.now()) != ""
}

// The ABI cannot delegate a filtered mixed-provider pool. When protection is
// needed, choose a safe host-offered candidate deterministically instead.
func (r *Router) pickReserveFallback(req PickRequest, d Decision, now time.Time) Decision {
	var usable []Candidate
	for _, c := range req.Candidates {
		if r.evaluate(c, req.Model, now).usable {
			usable = append(usable, c)
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
