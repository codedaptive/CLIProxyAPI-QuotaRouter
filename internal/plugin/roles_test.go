package plugin

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/roles"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestRoleUsageDoesNotCreateOrChangeConversationBindings(t *testing.T) {
	g := newRig(t, filepath.Join(t.TempDir(), "state"))
	g.host.set("claude", "A", "B")
	g.fetcher.set("A", 10, 24*time.Hour)
	g.fetcher.set("B", 20, 48*time.Hour)
	g.register("mode: active")
	g.refreshQuota()
	g.usage("claude", "opus", "A", "claude:desktop-chat", 200, nil)
	before := g.p.router.Export().Bindings
	g.p.Usage(pluginapi.UsageRecord{Provider: "claude", Model: "opus", AuthID: "B", APIKey: clientKey, SessionID: "claude:" + roles.SessionPrefix + "synthetic", RequestedAt: g.clock()})
	after := g.p.router.Export().Bindings
	if len(before) != 1 || len(after) != 1 || after[0].AuthID != "A" {
		t.Fatalf("bindings changed: %+v", after)
	}
}
