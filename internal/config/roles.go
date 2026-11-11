package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Role gives clients one stable model name with ordered fallback targets.
type Role struct {
	Name                      string       `yaml:"name" json:"name"`
	Enabled                   bool         `yaml:"enabled" json:"enabled"`
	AllowClientEffortOverride bool         `yaml:"allow_client_effort_override" json:"allow_client_effort_override"`
	CooldownSeconds           int          `yaml:"cooldown_seconds" json:"cooldown_seconds"`
	Targets                   []RoleTarget `yaml:"targets" json:"targets"`
}
type RoleTarget struct {
	Model      string            `yaml:"model" json:"model"`
	Effort     string            `yaml:"effort" json:"effort"`
	Providers  []string          `yaml:"providers,omitempty" json:"providers,omitempty"`
	AccountIDs []string          `yaml:"account_ids,omitempty" json:"account_ids,omitempty"`
	EffortMap  map[string]string `yaml:"effort_map,omitempty" json:"effort_map,omitempty"`
}

var roleProviderPattern = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,119}$`)

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,79}$`)

func ValidEffort(s string) bool {
	switch s {
	case "auto", "none", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}
func validateRoles(roles []Role) Errors {
	var errs Errors
	add := func(i int, message string) {
		errs = append(errs, FieldError{Field: fmt.Sprintf("roles.%d", i), Message: message})
	}
	if len(roles) > 100 {
		errs = append(errs, FieldError{Field: "roles", Message: "at most 100 roles"})
	}
	names := map[string]bool{}
	for i, r := range roles {
		if !roleNamePattern.MatchString(r.Name) || names[r.Name] {
			add(i, "role names must be unique lowercase names, digits, hyphens or underscores")
		}
		names[r.Name] = true
	}
	for i, r := range roles {
		if len(r.Targets) == 0 || len(r.Targets) > 16 {
			add(i, "each role requires 1–16 ordered targets")
		}
		if r.CooldownSeconds < 0 || r.CooldownSeconds > 300 {
			add(i, "cooldown_seconds must be 0–300 (0 uses 30 seconds)")
		}
		for _, t := range r.Targets {
			if strings.TrimSpace(t.Model) != t.Model || t.Model == "" || len(t.Model) > 200 || strings.ContainsAny(t.Model, "()\r\n\t") || names[t.Model] {
				add(i, "target must be a real model name without effort suffix, not another role")
			}
			if !ValidEffort(t.Effort) {
				add(i, "target effort must be auto, none, low, medium, high, xhigh or max")
			}
			if len(t.AccountIDs) > 1000 || len(t.Providers) > 32 {
				add(i, "too many account/provider restrictions")
			}
			for _, id := range t.AccountIDs {
				if !validAuthID(id) {
					add(i, "invalid account ID")
				}
			}
			for _, p := range t.Providers {
				if !roleProviderPattern.MatchString(p) || p == PluginID {
					add(i, "invalid target provider")
				}
			}
			for from, to := range t.EffortMap {
				if !ValidEffort(from) || !ValidEffort(to) || from == "auto" {
					add(i, "effort_map requires explicit supported effort labels")
				}
			}
		}
	}
	return errs
}
func publicRoles(roles []Role) []map[string]any {
	out := []map[string]any{}
	for _, r := range roles {
		b, _ := json.Marshal(r)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		out = append(out, v)
	}
	return out
}
