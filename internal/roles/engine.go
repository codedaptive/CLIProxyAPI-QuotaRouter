// Package roles executes task roles through CPA's authenticated host callbacks.
// It never reads upstream credentials or implements provider HTTP transports.
package roles

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type Host interface {
	List() ([]pluginapi.HostAuthFileEntry, error)
	Call(string, any, any) error
}
type Eligibility interface {
	RoleEligible(string, string, string) bool
}

// ReserveEligibility adds last-resort accounts without changing normal role
// eligibility. They remain behind every ordinary account for the same target.
type ReserveEligibility interface {
	RoleReserveEligible(string, string, string) bool
}
type Request struct {
	pluginapi.ExecutorRequest
	StreamID       string `json:"stream_id"`
	HostCallbackID string `json:"host_callback_id"`
}
type callbackRequest struct {
	pluginapi.HostModelExecutionRequest
	HostCallbackID string `json:"host_callback_id"`
}
type Decision struct {
	Role      string    `json:"role"`
	Model     string    `json:"model"`
	Effort    string    `json:"effort"`
	Provider  string    `json:"provider"`
	AccountID string    `json:"account_id"`
	Target    int       `json:"target"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	At        time.Time `json:"at"`
}
type cooldown struct {
	Until time.Time
	Probe bool
}
type Engine struct {
	Host        Host
	Eligibility Eligibility
	Now         func() time.Time
	mu          sync.Mutex
	roles       []config.Role
	active      bool
	cooldowns   map[string]cooldown
	decisions   []Decision
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
func (e *Engine) Configure(roles []config.Role, active bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.roles = roles
	e.active = active
	if e.cooldowns == nil {
		e.cooldowns = map[string]cooldown{}
	}
}
func splitRole(name string) (string, string) {
	if i := strings.LastIndex(name, "("); i > 0 && strings.HasSuffix(name, ")") {
		return name[:i], name[i+1 : len(name)-1]
	}
	return name, ""
}
func (e *Engine) role(name string) (config.Role, bool) {
	base, _ := splitRole(name)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.roles {
		if r.Name == base && r.Enabled && e.active {
			return r, true
		}
	}
	return config.Role{}, false
}
func (e *Engine) Handles(name string) bool { _, ok := e.role(name); return ok }
func (e *Engine) Models() pluginapi.ModelRegistrationResponse {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := pluginapi.ModelRegistrationResponse{Provider: config.PluginID, Models: []pluginapi.ModelInfo{}}
	if e.active {
		for _, r := range e.roles {
			if r.Enabled {
				out.Models = append(out.Models, pluginapi.ModelInfo{ID: r.Name, Object: "model", OwnedBy: config.PluginID, DisplayName: r.Name, Description: "Role routing: ordered model targets with automatic recovery", UserDefined: true, Thinking: &pluginapi.ThinkingSupport{Levels: []string{"low", "medium", "high", "xhigh", "max"}, DynamicAllowed: true, ZeroAllowed: true}})
			}
		}
	}
	return out
}
func (e *Engine) Decisions() []Decision {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Decision{}, e.decisions...)
}
func (e *Engine) record(d Decision) {
	e.mu.Lock()
	defer e.mu.Unlock()
	d.At = e.now()
	e.decisions = append(e.decisions, d)
	if len(e.decisions) > 100 {
		e.decisions = e.decisions[len(e.decisions)-100:]
	}
}
func failure(code, message string, status int) error {
	return pluginabi.NewError(code, message, status)
}
func status(err error) int {
	var s interface{ StatusCode() int }
	if errors.As(err, &s) {
		return s.StatusCode()
	}
	return 0
}

type terminalError struct{ error }

func (e terminalError) Unwrap() error { return e.error }
func retryable(err error) bool {
	var terminal terminalError
	if errors.As(err, &terminal) {
		return false
	}
	return availabilityFailure(err)
}
func availabilityFailure(err error) bool {
	s := status(err)
	return s == 401 || s == 403 || s == 404 || s == 408 || s == 429 || s == 500 || s == 502 || s == 503 || s == 504
}
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

type candidate struct {
	ID, Provider string
	Priority     int
	Reserved     bool
}

func (e *Engine) catalog() ([]pluginapi.HostAuthModels, map[string]pluginapi.HostAuthFileEntry, error) {
	var models pluginapi.HostAuthModelsResponse
	if err := e.Host.Call(pluginabi.MethodHostAuthModels, pluginapi.HostAuthModelsRequest{}, &models); err != nil {
		return nil, nil, err
	}
	files, err := e.Host.List()
	if err != nil {
		return nil, nil, err
	}
	roster := map[string]pluginapi.HostAuthFileEntry{}
	for _, f := range files {
		roster[f.ID] = f
	}
	return models.Credentials, roster, nil
}
func (e *Engine) candidates(t config.RoleTarget, models []pluginapi.HostAuthModels, roster map[string]pluginapi.HostAuthFileEntry) []candidate {
	var out []candidate
	for _, m := range models {
		if m.Provider == config.PluginID || !contains(m.Models, t.Model) || (len(t.Providers) > 0 && !contains(t.Providers, m.Provider)) || (len(t.AccountIDs) > 0 && !contains(t.AccountIDs, m.ID)) {
			continue
		}
		f := roster[m.ID]
		// Config-backed API credentials may appear in host.auth.models but not
		// host.auth.list. The exact auth pin lets CPA validate their live state.
		if f.Disabled || (f.Unavailable && f.NextRetryAfter.After(e.now())) {
			continue
		}
		reserved := false
		if e.Eligibility != nil && !e.Eligibility.RoleEligible(m.ID, m.Provider, t.Model) {
			fallback, ok := e.Eligibility.(ReserveEligibility)
			if !ok || !fallback.RoleReserveEligible(m.ID, m.Provider, t.Model) {
				continue
			}
			reserved = true
		}
		out = append(out, candidate{ID: m.ID, Provider: m.Provider, Priority: f.Priority, Reserved: reserved})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reserved != out[j].Reserved {
			return !out[i].Reserved
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}
func (e *Engine) acquire(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.cooldowns[key]
	if !ok {
		return true
	}
	if c.Probe || c.Until.After(e.now()) {
		return false
	}
	c.Probe = true
	e.cooldowns[key] = c
	return true
}
func (e *Engine) release(key string, err error, seconds int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err == nil || !availabilityFailure(err) {
		delete(e.cooldowns, key)
		return
	}
	if seconds == 0 {
		seconds = 30
	}
	// Bound memory; old cooldown entries have no durable meaning after recovery.
	if len(e.cooldowns) >= 4096 {
		for k, c := range e.cooldowns {
			if !c.Probe && c.Until.Before(e.now()) {
				delete(e.cooldowns, k)
			}
		}
	}
	if len(e.cooldowns) < 4096 {
		e.cooldowns[key] = cooldown{Until: e.now().Add(time.Duration(seconds) * time.Second)}
	} else {
		delete(e.cooldowns, key)
	}
}

// effectiveBody resolves effort once for each target. A suffix on the REAL model
// uses CPA's canonical effort pipeline, which overrides conflicting client body
// controls without duplicating provider-specific thinking conversions here.
func effectiveBody(req Request, role config.Role, t config.RoleTarget) ([]byte, string, string, error) {
	body := req.OriginalRequest
	if len(body) == 0 {
		body = req.Payload
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(body, &data); err != nil || data == nil {
		return nil, "", "", failure("invalid_role_request", "Role request must be a JSON object", 400)
	}
	effort := t.Effort
	if role.AllowClientEffortOverride {
		_, incoming := splitRole(req.Model)
		if incoming == "" {
			var output struct {
				Effort string `json:"effort"`
			}
			_ = json.Unmarshal(data["output_config"], &output)
			incoming = output.Effort
			if incoming == "" {
				var reasoning struct {
					Effort string `json:"effort"`
				}
				_ = json.Unmarshal(data["reasoning"], &reasoning)
				incoming = reasoning.Effort
			}
			if incoming == "" {
				_ = json.Unmarshal(data["reasoning_effort"], &incoming)
			}
		}
		if incoming != "" && incoming != "auto" {
			if !config.ValidEffort(incoming) {
				return nil, "", "", failure("invalid_effort", "Unsupported client effort label", 400)
			}
			effort = incoming
			if mapped := t.EffortMap[incoming]; mapped != "" {
				effort = mapped
			}
		}
	}
	model := t.Model
	if effort != "auto" {
		model += "(" + effort + ")"
	}
	// Remove competing thinking fields. Auto means the model's own default; fixed
	// roles must not inherit a client's budget/disabled flag or previous effort.
	delete(data, "thinking")
	delete(data, "reasoning_effort")
	for _, field := range []string{"output_config", "reasoning"} {
		var obj map[string]json.RawMessage
		if json.Unmarshal(data[field], &obj) == nil && obj != nil {
			delete(obj, "effort")
			if len(obj) == 0 {
				delete(data, field)
			} else {
				data[field], _ = json.Marshal(obj)
			}
		}
	}
	data["model"], _ = json.Marshal(model)
	out, err := json.Marshal(data)
	return out, model, effort, err
}

// Execute makes no fallback attempt after the first non-empty stream chunk.
// Host calls inherit the original authenticated request context via callback ID.
func (e *Engine) Execute(req Request) (any, error) {
	role, ok := e.role(req.Model)
	if !ok {
		return nil, failure("unknown_role", "Role is disabled or not configured", 404)
	}
	if e.Host == nil || req.HostCallbackID == "" {
		return nil, failure("role_host_unavailable", "Authenticated host execution context is unavailable", 503)
	}
	models, roster, err := e.catalog()
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		if m.Provider != config.PluginID && contains(m.Models, role.Name) {
			return nil, failure("role_name_conflict", "Role name conflicts with an existing model or alias", 409)
		}
	}
	var last error
	for i, t := range role.Targets {
		body, model, effort, errBody := effectiveBody(req, role, t)
		if errBody != nil {
			return nil, errBody
		}
		for _, c := range e.candidates(t, models, roster) {
			key := c.Provider + "\x00" + c.ID + "\x00" + t.Model + "\x00" + effort
			if !e.acquire(key) {
				continue
			}
			d := Decision{Role: role.Name, Model: t.Model, Effort: effort, Provider: c.Provider, AccountID: c.ID, Target: i + 1}
			format := req.SourceFormat
			if format == "" {
				format = req.Format
			}
			if format == "" {
				format = "openai"
			}
			call := callbackRequest{HostCallbackID: req.HostCallbackID, HostModelExecutionRequest: pluginapi.HostModelExecutionRequest{EntryProtocol: format, ExitProtocol: format, Model: model, Stream: req.Stream, Body: body, Headers: roleHeaders(req, role.Name), Query: req.Query, AuthID: c.ID}}
			var result any
			if req.Stream {
				result, err = e.stream(req, call, d)
			} else {
				var response pluginapi.HostModelExecutionResponse
				err = e.Host.Call(pluginabi.MethodHostModelExecute, call, &response)
				if err == nil && response.StatusCode >= 400 {
					err = failure("upstream_error", "Upstream request failed", response.StatusCode)
				}
				if err == nil {
					result = pluginapi.ExecutorResponse{Payload: response.Body, Headers: diagnosticHeaders(response.Headers, d)}
				}
			}
			e.release(key, err, role.CooldownSeconds)
			if err == nil {
				d.Status = "selected"
				d.Reason = "preferred target"
				if i > 0 {
					d.Reason = "earlier targets unavailable"
				}
				if c.Reserved {
					d.Reason = "weekly_reserve_last_available"
				}
				e.record(d)
				return result, nil
			}
			d.Status = "failed"
			d.Reason = fmt.Sprintf("HTTP %d", status(err))
			e.record(d)
			if !retryable(err) {
				return nil, err
			}
			last = err
		}
	}
	if last != nil {
		return nil, last
	}
	e.record(Decision{Role: role.Name, Status: "unavailable", Reason: "No eligible target account; quota, cooldown, model support or restrictions"})
	return nil, failure("role_unavailable", "No eligible account can currently serve this role", 503)
}
func diagnosticHeaders(h http.Header, d Decision) http.Header {
	out := h.Clone()
	if out == nil {
		out = make(http.Header)
	}
	out.Set("X-Role-Name", d.Role)
	out.Set("X-Role-Model", d.Model)
	out.Set("X-Role-Effort", d.Effort)
	return out
}
