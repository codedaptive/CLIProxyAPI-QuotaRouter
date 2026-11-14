package router

import "github.com/kpatdev/quota-router/internal/quota"

// RoleEligible checks quota/health without reading or mutating conversation
// bindings. The host still validates model support and exact-credential pins.
func (r *Router) RoleEligible(id, provider, model string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !supportedProvider(normalizeProvider(provider)) {
		return true
	}
	if r.weeklyReserveCause(id, provider, r.now()) != "" {
		return false
	}
	a := r.accounts[id]
	if a == nil {
		return true
	} // host may have discovered a new credential since our last poll
	return r.evaluate(Candidate{ID: id, Provider: provider, Priority: a.Priority, Status: a.HostStatus}, model, r.now()).usable
}

// RoleReserveEligible is used only after ordinary accounts for a role target
// have been tried. It bypasses the percentage floor, never quota/health blocks.
func (r *Router) RoleReserveEligible(id, provider, model string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if r.weeklyReserveCause(id, provider, now) != "weekly_reserve_protected" {
		return false
	}
	a := r.accounts[id]
	if a == nil || !a.Present || a.Disabled || (a.Unavailable && a.CooldownUntil.After(now)) {
		return false
	}
	e := r.evaluateReserve(Candidate{ID: id, Provider: provider, Priority: a.Priority, Status: a.HostStatus}, model, now, true)
	return e.usable && e.assessment.State == quota.Ready
}
