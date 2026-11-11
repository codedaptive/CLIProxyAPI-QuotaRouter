package session

import "testing"

func TestCallerScopeAndHostCanonicalIdentity(t *testing.T) {
	scope := CallerScope("synthetic-client")
	r, ok := FromRequest(nil, map[string]any{"caller_scope": scope, "canonical_session_id": "claude:parent:agent:child", "parent_session_id": "claude:parent"})
	if !ok || r.CallerScope != scope || r.Kind != Subagent {
		t.Fatalf("host identity not preserved: %+v", r)
	}
	u, uok := FromUsage(r.ID, r.ParentID, "synthetic-client")
	if !uok || u.Key != r.Key || u.CallerScope != r.CallerScope {
		t.Fatal("request/usage identity mismatch")
	}
	r, ok = FromRequest(nil, map[string]any{"caller_scope": scope, "canonical_session_id": "clientreq:per-request-id"})
	if ok || r.CallerScope != scope {
		t.Fatal("request ID became session or caller scope lost")
	}
}
