package router

import (
	"sort"
	"strings"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/kpatdev/quota-router/internal/quota"
	"github.com/kpatdev/quota-router/internal/session"
)

func (r *Router) desktop(ref session.Ref) bool { return r.cfg.ClientRouting.Desktop(ref.CallerScope) }

func (r *Router) affinity(ref session.Ref) bool {
	if r.cfg.ClientRouting.Enabled {
		return r.desktop(ref)
	}
	return r.cfg.AffinityEnabled
}

func (r *Router) requestScope(model string, ref session.Ref) string {
	if r.desktop(ref) {
		return "*"
	}
	return r.scope(model)
}

// Desktop bindings survive idle periods. They remain bounded by the existing
// LRU cap and may be cleared explicitly from the management UI.
func (r *Router) expired(b *Binding, now time.Time) bool {
	return !b.Desktop && now.Sub(b.LastSeen) >= r.cfg.AffinityTTL
}

func (r *Router) rebindPolicy(ref session.Ref) string {
	if r.desktop(ref) {
		return config.RebindConservative
	}
	return r.cfg.RebindPolicy
}

func (r *Router) planRank(c Candidate) int {
	plans := r.cfg.ClientRouting.PreferredPlans[normalizeProvider(c.Provider)]
	plan := r.cfg.ClientRouting.AccountPlans[c.ID]
	if plan == "" {
		if a := r.accounts[c.ID]; a != nil {
			plan = a.PlanType
		}
	}
	plan = config.NormalizePlan(plan)
	for i, preferred := range plans {
		if config.NormalizePlan(preferred) == plan {
			return i
		}
	}
	return len(plans)
}

func (r *Router) rankForRequest(order []*eval, now time.Time, req PickRequest) []Ranked {
	strategy := r.cfg.Strategy
	if r.desktop(req.Session) {
		strategy = config.StrategyResetFirst
	}
	ranked := r.rank(order, now, strategy)
	if !r.desktop(req.Session) {
		return ranked
	}
	byID := map[string]Candidate{}
	for _, e := range order {
		byID[e.c.ID] = e.c
	}
	sort.SliceStable(ranked, func(i, j int) bool { return r.planRank(byID[ranked[i].AuthID]) < r.planRank(byID[ranked[j].AuthID]) })
	for i := range ranked {
		ranked[i].Reason = "desktop plan preference; " + ranked[i].Reason
	}
	return ranked
}

func (r *Router) chooseForRequest(order []*eval, ranked []Ranked, exclude string, req PickRequest) (string, bool) {
	if !r.desktop(req.Session) {
		return r.choose(order, ranked, exclude)
	}
	for _, rk := range ranked {
		if rk.AuthID != exclude {
			return rk.AuthID, false
		}
	}
	// When quota is missing, still respect plan preference among candidates
	// offered by the host; never resurrect an exhausted or rejected account.
	var usable []Candidate
	for _, e := range order {
		if e.usable && e.c.ID != exclude {
			usable = append(usable, e.c)
		}
	}
	sort.Slice(usable, func(i, j int) bool {
		a, b := usable[i], usable[j]
		if r.planRank(a) != r.planRank(b) {
			return r.planRank(a) < r.planRank(b)
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.ID < b.ID
	})
	if len(usable) > 0 {
		return usable[0].ID, true
	}
	return "", false
}

// pickLegacy preserves quota-reset-router's CLI selection contract: the host's
// highest candidate priority, then earliest account weekly/monthly reset, then
// ID. No session bindings, headroom reservation, or capacity tie-breaker.
func (r *Router) pickLegacy(req PickRequest, d Decision, order []*eval, now time.Time) Decision {
	d.Policy = "cli_priority_reset_first"
	if req.PinnedAuthID != "" {
		for _, e := range order {
			if e.c.ID == req.PinnedAuthID {
				if strings.HasPrefix(e.cause, "weekly_reserve_") {
					return r.finish(d)
				}
				d.AuthID = req.PinnedAuthID
				d.Outcome = OutcomeHostPinned
				return r.finish(d)
			}
		}
	}
	if len(d.reserveReleased) > 0 {
		// Unusable higher-priority accounts must not prevent last-resort routing.
		var ready []*eval
		for _, e := range order {
			if e.usable {
				ready = append(ready, e)
			}
		}
		order = ready
	}
	d.Ranked = r.legacyRank(order, now)
	if len(d.Ranked) == 0 {
		d.Outcome = OutcomeDelegated
		d.Cause = "no_known_usable_quota"
	} else {
		d.Outcome = OutcomeNoSession
		d.AuthID = d.Ranked[0].AuthID
		d.Cause = "cli_per_request"
	}
	return r.finish(d)
}

func (r *Router) legacyRank(order []*eval, now time.Time) []Ranked {
	var ranked []Ranked
	highest := 0
	havePriority := false
	for _, e := range order {
		if strings.HasPrefix(e.cause, "weekly_reserve_") {
			continue
		}
		if !havePriority || e.c.Priority > highest {
			havePriority = true
			highest = e.c.Priority
		}
	}
	type choice struct {
		id    string
		reset time.Time
	}
	var choices []choice
	for _, e := range order {
		if e.c.Priority != highest {
			continue
		}
		a := r.accounts[e.c.ID]
		if !e.usable || a == nil || !a.HasSnapshot || e.assessment.State != quota.Ready {
			continue
		}
		var reset time.Time
		for _, w := range a.Snapshot.Windows {
			if (w.Kind == quota.Weekly || w.Kind == quota.Monthly) && w.ResetsAt != nil && w.ResetsAt.After(now) && (reset.IsZero() || w.ResetsAt.Before(reset)) {
				reset = *w.ResetsAt
			}
		}
		if !reset.IsZero() {
			choices = append(choices, choice{e.c.ID, reset})
		}
	}
	sort.Slice(choices, func(i, j int) bool {
		if !choices[i].reset.Equal(choices[j].reset) {
			return choices[i].reset.Before(choices[j].reset)
		}
		return choices[i].id < choices[j].id
	})
	for _, c := range choices {
		ranked = append(ranked, Ranked{AuthID: c.id, Reason: "CLI priority, earliest weekly/monthly reset, account ID", State: string(quota.Ready)})
	}
	return ranked
}
