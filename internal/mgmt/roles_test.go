package mgmt

import (
	"strings"
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/kpatdev/quota-router/internal/router"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestDiagnosticsRedactRoleAndDesktopAccountIDs(t *testing.T) {
	cfg := config.Default()
	cfg.Roles = []config.Role{{Name: "designer", Targets: []config.RoleTarget{{Model: "opus", Effort: "high", AccountIDs: []string{"private@example.com.json"}}}}}
	cfg.ClientRouting.AccountPlans = map[string]string{"private@example.com.json": "max"}
	cfg.ClientRouting.DesktopCallerScopes = []string{strings.Repeat("a", 64)}
	r := router.New(cfg, time.Now)
	s := Server{Router: r, Now: time.Now, Info: func() RuntimeInfo { return RuntimeInfo{} }}
	out := s.export(pluginapi.ManagementRequest{})
	if strings.Contains(string(out.Body), "private@example.com") || strings.Contains(string(out.Body), strings.Repeat("a", 64)) {
		t.Fatal(string(out.Body))
	}
	if r.Config().Roles[0].Targets[0].AccountIDs[0] != "private@example.com.json" {
		t.Fatal("export mutated configuration")
	}
}
