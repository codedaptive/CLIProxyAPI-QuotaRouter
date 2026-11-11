package plugin

import (
	"encoding/json"
	"errors"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/kpatdev/quota-router/internal/roles"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func roleError(err error) []byte {
	var api *pluginabi.Error
	if !errors.As(err, &api) {
		api = pluginabi.NewError("role_execution_failed", "Role execution failed", 502)
	}
	out, _ := json.Marshal(pluginabi.Envelope{Error: api})
	return out
}
func (p *Runtime) handleRoles(method string, raw []byte) []byte {
	p.mu.Lock()
	e := p.roles
	p.mu.Unlock()
	if method == pluginabi.MethodExecutorIdentifier {
		return success(map[string]string{"identifier": config.PluginID})
	}
	if e == nil {
		return failure("roles_unavailable")
	}
	switch method {
	case pluginabi.MethodModelRegister:
		return success(e.Models())
	case pluginabi.MethodModelRoute:
		var req pluginapi.ModelRouteRequest
		if json.Unmarshal(raw, &req) != nil {
			return failure("invalid_role_request")
		}
		if !p.quiesced.Load() && e.Handles(req.RequestedModel) {
			if roles.IsCount(req.Metadata) {
				result, err := e.CountRoute(req)
				if err != nil {
					return roleError(err)
				}
				return success(result)
			}
			return success(pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetSelf, Reason: "role routing"})
		}
		return success(pluginapi.ModelRouteResponse{})
	case pluginabi.MethodExecutorCountTokens:
		return roleError(pluginabi.NewError("role_count_unsupported", "Token counting through role names is not available; use the concrete model", 501))
	default:
		if p.quiesced.Load() {
			return roleError(pluginabi.NewError("role_draining", "Role router is draining", 503))
		}
		var req roles.Request
		if json.Unmarshal(raw, &req) != nil {
			return failure("invalid_role_request")
		}
		req.Stream = method == pluginabi.MethodExecutorExecuteStream
		var result any
		var err error
		if req.Stream {
			result, err = e.ExecuteStream(req)
		} else {
			result, err = e.Execute(req)
		}
		if err != nil {
			return roleError(err)
		}
		return success(result)
	}
}
