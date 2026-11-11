package plugin

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/roles"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestMaxReserveThroughSchedulerRPC(t *testing.T) {
	g := newRig(t, filepath.Join(t.TempDir(), "state"))
	g.host.set("claude", "A", "B")
	g.fetcher.set("A", 90, 48*time.Hour)
	g.fetcher.set("B", 20, 96*time.Hour)
	g.register("mode: active\nmax_weekly_reserve: {enabled: true}\nclient_routing: {account_plans: {A: ultra}}\n")
	g.refreshQuota()
	req := pluginapi.SchedulerPickRequest{Provider: "claude", Model: "model", Candidates: []pluginapi.SchedulerAuthCandidate{{ID: "A", Provider: "claude", Priority: 10}, {ID: "B", Provider: "claude", Priority: 1}}}
	call := func() pluginapi.SchedulerPickResponse {
		var resp pluginapi.SchedulerPickResponse
		if err := json.Unmarshal(g.call(pluginabi.MethodSchedulerPick, req), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if resp := call(); !resp.Handled || resp.Reject || resp.AuthID != "B" {
		t.Fatalf("priority bypass: %+v", resp)
	}
	req.Candidates = req.Candidates[:1]
	if resp := call(); !resp.Handled || !resp.Reject || resp.RejectCode != "weekly_reserve_protected" || resp.AuthID != "" {
		t.Fatalf("must not delegate: %+v", resp)
	}
	req.Options = pluginapi.SchedulerOptions{Headers: map[string][]string{"X-Claude-Code-Session-Id": {roles.SessionPrefix + "test"}}, Metadata: map[string]any{"pinned_auth_id": "A"}}
	if resp := call(); !resp.Handled || !resp.Reject || resp.AuthID != "" {
		t.Fatalf("role pin bypass: %+v", resp)
	}
	req.Options = pluginapi.SchedulerOptions{}
	// Live settings changes release the guard without restarting the router.
	g.register("mode: active\nmax_weekly_reserve: {enabled: false}\nclient_routing: {account_plans: {A: ultra}}\n")
	if resp := call(); !resp.Handled || resp.Reject || resp.AuthID != "A" {
		t.Fatalf("disable: %+v", resp)
	}
}
