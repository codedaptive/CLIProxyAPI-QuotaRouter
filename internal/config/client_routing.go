package config

import (
	"regexp"
	"strings"
)

// ClientRouting selects an opt-in desktop policy using authenticated caller
// scopes, never a spoofable User-Agent or a raw key stored in plugin settings.
type ClientRouting struct {
	Enabled             bool                `yaml:"enabled" json:"enabled"`
	DesktopCallerScopes []string            `yaml:"desktop_caller_scopes" json:"desktop_caller_scopes"`
	PreferredPlans      map[string][]string `yaml:"preferred_plans" json:"preferred_plans"`
	AccountPlans        map[string]string   `yaml:"account_plans" json:"account_plans"`
}

func defaultClientRouting() ClientRouting {
	return ClientRouting{
		DesktopCallerScopes: []string{},
		PreferredPlans:      map[string][]string{"claude": {"max"}, "codex": {"pro"}},
		AccountPlans:        map[string]string{},
	}
}

func (c ClientRouting) Desktop(scope string) bool {
	if !c.Enabled || scope == "" {
		return false
	}
	for _, s := range c.DesktopCallerScopes {
		if scope == s {
			return true
		}
	}
	return false
}

// NormalizePlan groups provider variants while retaining the provider boundary.
// Ultra is the current Claude Max name; retain the canonical max value so
// existing preferences and reserve configuration remain compatible.
func NormalizePlan(plan string) string {
	p := strings.ToLower(strings.TrimSpace(plan))
	switch p {
	case "max_5x", "max_20x", "max5", "max20", "max_5", "max_20", "claude_max",
		"ultra", "claude_ultra", "ultra_5x", "ultra_20x":
		return "max"
	case "chatgpt_pro":
		return "pro"
	case "chatgpt_plus":
		return "plus"
	}
	return p
}

var scopePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var planPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)

func (c ClientRouting) validate() Errors {
	var errs Errors
	add := func(field, message string) {
		errs = append(errs, FieldError{Field: "client_routing." + field, Message: message})
	}
	if len(c.DesktopCallerScopes) > 1000 {
		add("desktop_caller_scopes", "at most 1000 scopes")
	}
	seen := map[string]bool{}
	for _, s := range c.DesktopCallerScopes {
		if !scopePattern.MatchString(s) || seen[s] {
			add("desktop_caller_scopes", "must contain unique lowercase SHA-256 caller scopes, not API keys")
		}
		seen[s] = true
	}
	for p, plans := range c.PreferredPlans {
		if p != "claude" && p != "codex" {
			add("preferred_plans", "only claude and codex providers are supported")
		}
		if len(plans) > 20 {
			add("preferred_plans", "at most 20 plan tiers per provider")
		}
		for _, plan := range plans {
			if !planPattern.MatchString(plan) {
				add("preferred_plans", "invalid plan label")
			}
		}
	}
	if len(c.AccountPlans) > 1000 {
		add("account_plans", "at most 1000 account overrides")
	}
	for id, plan := range c.AccountPlans {
		if !validAuthID(id) || !planPattern.MatchString(plan) {
			add("account_plans", "invalid account ID or plan label")
		}
	}
	return errs
}

func (c ClientRouting) public() map[string]any {
	return map[string]any{"enabled": c.Enabled, "desktop_caller_scopes": c.DesktopCallerScopes, "preferred_plans": c.PreferredPlans, "account_plans": c.AccountPlans}
}
