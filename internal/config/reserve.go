package config

import (
	"math"
	"time"
)

// MaxWeeklyReserve protects Claude Max's account-wide weekly quota, not its
// five-hour or model-specific windows. Disabled by default for existing installs.
type MaxWeeklyReserve struct {
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	RemainingPercent float64       `yaml:"remaining_percent" json:"remaining_percent"`
	ReleaseBefore    time.Duration `yaml:"release_before" json:"-"`
}

func (c MaxWeeklyReserve) validate() Errors {
	var errs Errors
	if math.IsNaN(c.RemainingPercent) || c.RemainingPercent <= 0 || c.RemainingPercent >= 100 {
		errs = append(errs, FieldError{Field: "max_weekly_reserve.remaining_percent", Message: "must be greater than 0 and less than 100"})
	}
	if c.ReleaseBefore < time.Hour || c.ReleaseBefore > 7*24*time.Hour {
		errs = append(errs, FieldError{Field: "max_weekly_reserve.release_before", Message: "must be between 1h and 168h"})
	}
	return errs
}
func (c MaxWeeklyReserve) public() map[string]any {
	return map[string]any{"enabled": c.Enabled, "remaining_percent": c.RemainingPercent, "release_before": c.ReleaseBefore.String()}
}
