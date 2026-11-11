package router

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
