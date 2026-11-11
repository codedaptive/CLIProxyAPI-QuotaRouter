package plugin

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/session"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestDesktopPolicyThroughRPCAndDiskRestart(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			largePlan := "pro"
			smallPlan := "plus"
			if provider == "claude" {
				largePlan = "max"
				smallPlan = "pro"
			}
			cfg := "mode: active\nclient_routing:\n  enabled: true\n  desktop_caller_scopes: [" + session.CallerScope(clientKey) + "]\n  account_plans: {A: " + largePlan + ", B: " + smallPlan + "}\n"
			g := newRig(t, dir)
			g.host.set(provider, "A", "B")
			g.fetcher.set("A", 20, 96*time.Hour)
			g.fetcher.set("B", 20, 20*time.Hour)
			g.register(cfg)
			g.refreshQuota()
			h := headersFor(provider, "desktop-chat")
			if got := g.pick(provider, "model", h, "A", "B"); got != "A" {
				t.Fatalf("desktop chose %s", got)
			}
			// Same protocol/session headers, different authenticated caller scope: CLI routing.
			req := pluginapi.SchedulerPickRequest{Provider: provider, Providers: []string{provider}, Model: "model", Options: pluginapi.SchedulerOptions{Headers: h, Metadata: map[string]any{"caller_scope": session.CallerScope("cli-key")}}, Candidates: []pluginapi.SchedulerAuthCandidate{{ID: "A", Provider: provider}, {ID: "B", Provider: provider}}}
			var resp pluginapi.SchedulerPickResponse
			if err := json.Unmarshal(g.call(pluginabi.MethodSchedulerPick, req), &resp); err != nil || resp.AuthID != "B" {
				t.Fatalf("CLI routing: %+v %v", resp, err)
			}
			g.usage(provider, "model", "A", provider+":desktop-chat", 200, nil)
			// Manual quota refresh is debounced; model the next poll rather than
			// issuing a second refresh at the exact same clock tick.
			g.advance(time.Minute)
			g.fetcher.set("A", 100, 96*time.Hour)
			g.refreshQuota()
			if got := g.pick(provider, "model", h, "A", "B"); got != "B" {
				t.Fatalf("quota handoff: %s", got)
			}
			g.usage(provider, "model", "B", provider+":desktop-chat", 200, nil)
			g.p.Shutdown()
			restarted := newRig(t, dir)
			restarted.advance(time.Minute)
			restarted.host.set(provider, "A", "B")
			restarted.fetcher.set("A", 0, 96*time.Hour)
			restarted.fetcher.set("B", 20, 20*time.Hour)
			restarted.register(cfg)
			restarted.refreshQuota()
			if got := restarted.pick(provider, "model", h, "A", "B"); got != "B" {
				t.Fatalf("restart/recovery snapped back: %s", got)
			}
			if got := restarted.pick(provider, "model", headersFor(provider, "new-chat"), "A", "B"); got != "A" {
				t.Fatalf("new desktop chat: %s", got)
			}
		})
	}
}
