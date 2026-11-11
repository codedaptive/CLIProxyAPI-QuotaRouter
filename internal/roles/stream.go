package roles

import (
	"encoding/json"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func streamFailure(raw string) error {
	var detail struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
	}
	if json.Unmarshal([]byte(raw), &detail) == nil && detail.StatusCode > 0 {
		return failure("upstream_stream_error", "Upstream stream failed", detail.StatusCode)
	}
	// An unclassified terminal error is not safe to retry (it may be cancellation).
	return failure("upstream_stream_error", "Upstream stream ended unexpectedly", 0)
}
func (e *Engine) closeOutput(id string, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	_ = e.Host.Call(pluginabi.MethodHostStreamClose, map[string]any{"stream_id": id, "error": message}, &struct{}{})
}

// Streaming selection runs asynchronously so CPA can cancel the callback scope
// even while a backend has not produced its first chunk.
func (e *Engine) ExecuteStream(req Request) (any, error) {
	if e.Host == nil {
		return nil, failure("role_host_unavailable", "Host execution callbacks are unavailable", 503)
	}
	if req.StreamID == "" || req.HostCallbackID == "" {
		return nil, failure("missing_stream_context", "Missing host stream context", 500)
	}
	role, ok := e.role(req.Model)
	if !ok {
		return nil, failure("unknown_role", "Role is disabled or not configured", 404)
	}
	go func() {
		defer func() {
			if recover() != nil {
				e.closeOutput(req.StreamID, failure("role_stream_error", "Role stream failed", 500))
			}
		}()
		_, err := e.Execute(req)
		e.closeOutput(req.StreamID, err)
	}()
	return struct {
		Headers http.Header `json:"headers"`
	}{Headers: http.Header{"X-Role-Name": {role.Name}}}, nil
}
func (e *Engine) stream(req Request, call callbackRequest, d Decision) (any, error) {
	var opened pluginapi.HostModelStreamResponse
	if err := e.Host.Call(pluginabi.MethodHostModelExecuteStream, call, &opened); err != nil {
		return nil, err
	}
	if opened.StreamID == "" {
		return nil, failure("empty_stream", "Host returned an empty stream identifier", 502)
	}
	defer func() {
		_ = e.Host.Call(pluginabi.MethodHostModelStreamClose, pluginapi.HostModelStreamCloseRequest{StreamID: opened.StreamID}, &struct{}{})
	}()
	committed := false
	for {
		var chunk pluginapi.HostModelStreamReadResponse
		err := e.Host.Call(pluginabi.MethodHostModelStreamRead, pluginapi.HostModelStreamReadRequest{StreamID: opened.StreamID}, &chunk)
		if err == nil && len(chunk.Payload) > 0 {
			if emitErr := e.Host.Call(pluginabi.MethodHostStreamEmit, map[string]any{"stream_id": req.StreamID, "payload": chunk.Payload}, &struct{}{}); emitErr != nil {
				// Client disconnects must never trigger a fresh upstream generation.
				return nil, failure("stream_delivery_stopped", "Stream delivery stopped", 499)
			}
			if !committed {
				d.Status = "streaming"
				d.Reason = "preferred target"
				if d.Target > 1 {
					d.Reason = "earlier targets unavailable"
				}
				e.record(d)
			}
			committed = true
		}
		if err == nil && chunk.Error != "" {
			err = streamFailure(chunk.Error)
		}
		if err != nil {
			if committed {
				code := status(err)
				if code == 0 {
					code = 502
				}
				return nil, terminalError{failure("stream_interrupted", "Stream interrupted after output began; no model switch attempted", code)}
			}
			return nil, err
		}
		if chunk.Done {
			if !committed {
				return nil, failure("empty_stream", "Upstream returned an empty stream", 502)
			}
			return struct{}{}, nil
		}
	}
}
