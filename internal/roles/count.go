package roles

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Token counting stays on CPA's native provider path; it must never be replaced
// with generation or a made-up token estimate. The scheduler applies restrictions.
func IsCount(meta map[string]any) bool {
	path, _ := meta["request_path"].(string)
	return strings.HasSuffix(path, "/count_tokens")
}
func (e *Engine) CountRoute(req pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, error) {
	role, ok := e.role(req.RequestedModel)
	if !ok {
		return pluginapi.ModelRouteResponse{}, nil
	}
	models, roster, err := e.catalog()
	if err != nil {
		return pluginapi.ModelRouteResponse{}, err
	}
	for _, t := range role.Targets {
		candidates := e.candidates(t, models, roster)
		if len(candidates) == 0 {
			continue
		}
		_, model, _, err := effectiveBody(Request{ExecutorRequest: pluginapi.ExecutorRequest{Model: req.RequestedModel, OriginalRequest: req.Body}}, role, t)
		if err != nil {
			return pluginapi.ModelRouteResponse{}, err
		}
		return pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetProvider, Target: candidates[0].Provider, TargetModel: model, Reason: "role token counting"}, nil
	}
	return pluginapi.ModelRouteResponse{}, failure("role_unavailable", "No account available for role token counting", 503)
}
func (e *Engine) CountPick(req pluginapi.SchedulerPickRequest) (pluginapi.SchedulerPickResponse, bool) {
	if !IsCount(req.Options.Metadata) {
		return pluginapi.SchedulerPickResponse{}, false
	}
	name, _ := req.Options.Metadata["requested_model"].(string)
	role, ok := e.role(name)
	if !ok {
		return pluginapi.SchedulerPickResponse{}, false
	}
	model, _ := splitRole(req.Model)
	for _, t := range role.Targets {
		if t.Model != model {
			continue
		}
		var models []pluginapi.HostAuthModels
		roster := map[string]pluginapi.HostAuthFileEntry{}
		for _, c := range req.Candidates {
			models = append(models, pluginapi.HostAuthModels{ID: c.ID, Provider: c.Provider, Models: []string{model}})
			roster[c.ID] = pluginapi.HostAuthFileEntry{ID: c.ID, Priority: c.Priority, Disabled: strings.EqualFold(c.Status, "disabled")}
		}
		if candidates := e.candidates(t, models, roster); len(candidates) > 0 {
			return pluginapi.SchedulerPickResponse{Handled: true, AuthID: candidates[0].ID}, true
		}
	}
	return pluginapi.SchedulerPickResponse{Handled: true, Reject: true, RejectReason: "No permitted account for role token counting"}, true
}
