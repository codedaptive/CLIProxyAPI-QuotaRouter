package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientRoutingValidationAndReload(t *testing.T) {
	scope := strings.Repeat("a", 64)
	raw := `client_routing:
  enabled: true
  desktop_caller_scopes: [` + scope + `]
  account_plans: {claude-personal.json: max_20x}
`
	c, err := Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !c.ClientRouting.Desktop(scope) || c.ClientRouting.Desktop("") || c.ClientRouting.PreferredPlans["codex"][0] != "pro" {
		t.Fatal("client policy defaults/matching failed")
	}
	next, err := DecodeJSONPatch(c, []byte(`{"mode":"active"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !next.ClientRouting.Desktop(scope) || next.ClientRouting.AccountPlans["claude-personal.json"] != "max_20x" {
		t.Fatal("unrelated settings edit lost client policy")
	}
	b, _ := json.Marshal(next.Public())
	if !strings.Contains(string(b), "client_routing") {
		t.Fatal("policy not exposed to settings/export")
	}
	for _, bad := range []string{
		"client_routing: {desktop_caller_scopes: [sk-raw-key]}",
		"client_routing: {preferred_plans: {unknown: [pro]}}",
		"client_routing: {account_plans: {test: 'bad label'}}",
		"client_routing: {enabled: true}\nschedule_across_priorities: false",
		"client_routing: {desktop_caller_scopes: [" + scope + ", " + scope + "]}",
	} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Fatalf("accepted invalid policy: %s", bad)
		}
	}
}

func TestUltraAndLegacyMaxPlanNamesAreEquivalent(t *testing.T) {
	for _, plan := range []string{"max", "max_5x", "max_20x", "claude_max", "Ultra", " ULTRA ", "claude_ultra", "ultra_5x", "ultra_20x"} {
		if got := NormalizePlan(plan); got != "max" {
			t.Fatalf("%q normalized to %q", plan, got)
		}
	}
	for _, plan := range []string{"pro", "team", "plus", "ultraviolet", "unknown"} {
		if got := NormalizePlan(plan); got != plan {
			t.Fatalf("unrelated plan %q changed to %q", plan, got)
		}
	}
	cfg, err := Decode([]byte("client_routing: {preferred_plans: {claude: [ultra]}, account_plans: {A: ultra}}"))
	if err != nil {
		t.Fatal(err)
	}
	next, err := DecodeJSONPatch(cfg, []byte(`{"strategy":"balanced"}`))
	if err != nil || next.ClientRouting.AccountPlans["A"] != "ultra" {
		t.Fatalf("Ultra lost on settings update: %v", err)
	}
}
