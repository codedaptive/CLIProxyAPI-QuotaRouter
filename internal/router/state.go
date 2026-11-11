package router

import (
	"sort"
	"time"

	"github.com/kpatdev/quota-router/internal/quota"
)

// StateVersion is bumped when the persisted shape changes incompatibly.
const StateVersion = 1

// AccountState is the persisted part of an account. It holds no credentials.
type AccountState struct {
	PollRetryAt  time.Time      `json:"poll_retry_at,omitempty"`
	PollFailures int            `json:"poll_failures,omitempty"`
	PollError    string         `json:"poll_error,omitempty"`
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	Display      string         `json:"display"`
	PlanType     string         `json:"plan_type,omitempty"`
	Identity     string         `json:"identity,omitempty"`
	Snapshot     quota.Snapshot `json:"snapshot"`
	HasSnapshot  bool           `json:"has_snapshot"`
	Health       health         `json:"health"`
}

// State is everything the router persists.
type State struct {
	Version  int            `json:"version"`
	Accounts []AccountState `json:"accounts"`
	Bindings []Binding      `json:"bindings"`
}

// Export copies persistent state.
func (r *Router) Export() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	s := State{Version: StateVersion}
	for _, a := range r.accounts {
		s.Accounts = append(s.Accounts, AccountState{
			ID: a.ID, Provider: a.Provider, Display: a.Display, PlanType: a.PlanType, Identity: a.Identity,
			Snapshot: a.Snapshot.Clone(), HasSnapshot: a.HasSnapshot, Health: cloneHealth(a.Health),
			PollRetryAt: a.NextPoll, PollFailures: a.PollFailures, PollError: a.PollError,
		})
	}
	for _, b := range r.bindings {
		if !r.expired(b, now) {
			s.Bindings = append(s.Bindings, *b)
		}
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].ID < s.Accounts[j].ID })
	sort.Slice(s.Bindings, func(i, j int) bool { return s.Bindings[i].Key < s.Bindings[j].Key })
	return s
}

// Import restores persisted state into an empty router. Accounts come back
// as not-present until the first roster sync confirms them, and every
// account is scheduled for an early poll.
func (r *Router) Import(s State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.Version != StateVersion {
		return
	}
	now := r.now()
	for _, as := range s.Accounts {
		if as.ID == "" || !supportedProvider(as.Provider) {
			continue
		}
		r.accounts[as.ID] = &account{
			ID: as.ID, Provider: as.Provider, Display: as.Display, PlanType: as.PlanType, Identity: as.Identity,
			Snapshot: as.Snapshot, HasSnapshot: as.HasSnapshot, Health: as.Health, NextPoll: now, Present: true,
		}
		// Preserve upstream cooldowns across library/config reloads. Older state
		// without these optional fields still gets the usual initial poll.
		if as.PollFailures > 0 && as.PollError == "quota_http_429" && as.PollRetryAt.After(now) {
			a := r.accounts[as.ID]
			a.NextPoll, a.PollFailures, a.PollError = as.PollRetryAt, as.PollFailures, as.PollError
		}
	}
	for i := range s.Bindings {
		b := s.Bindings[i]
		if b.Key == "" || b.AuthID == "" || r.expired(&b, now) || b.LastSeen.After(now.Add(time.Hour)) {
			continue
		}
		r.bindings[b.Key] = &b
	}
	r.evictLocked(now)
}

func cloneHealth(h health) health {
	if h.ModelBlocked != nil {
		m := make(map[string]time.Time, len(h.ModelBlocked))
		for k, v := range h.ModelBlocked {
			m[k] = v
		}
		h.ModelBlocked = m
	}
	return h
}
