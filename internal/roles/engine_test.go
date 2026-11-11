package roles

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kpatdev/quota-router/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type fakeHost struct {
	mu             sync.Mutex
	files          []pluginapi.HostAuthFileEntry
	models         []pluginapi.HostAuthModels
	calls          []callbackRequest
	failures       map[string]int
	chunks         map[string][]pluginapi.HostModelStreamReadResponse
	streamAccounts map[string]string
	emitted        [][]byte
	closed         chan string
	upstreamClosed []string
	emitError      bool
}

func fill(out, value any) error                                  { b, _ := json.Marshal(value); return json.Unmarshal(b, out) }
func (h *fakeHost) List() ([]pluginapi.HostAuthFileEntry, error) { return h.files, nil }
func (h *fakeHost) Call(method string, in, out any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch method {
	case pluginabi.MethodHostAuthModels:
		return fill(out, pluginapi.HostAuthModelsResponse{Credentials: h.models})
	case pluginabi.MethodHostModelExecute, pluginabi.MethodHostModelExecuteStream:
		req := in.(callbackRequest)
		h.calls = append(h.calls, req)
		if code := h.failures[req.AuthID]; code != 0 {
			return pluginabi.NewError("upstream", "synthetic failure", code)
		}
		if method == pluginabi.MethodHostModelExecute {
			return fill(out, pluginapi.HostModelExecutionResponse{StatusCode: 200, Body: []byte(`{"ok":true}`)})
		}
		id := fmt.Sprintf("stream-%d", len(h.calls))
		h.streamAccounts[id] = req.AuthID
		return fill(out, pluginapi.HostModelStreamResponse{StatusCode: 200, StreamID: id})
	case pluginabi.MethodHostModelStreamRead:
		id := in.(pluginapi.HostModelStreamReadRequest).StreamID
		account := h.streamAccounts[id]
		chunks := h.chunks[account]
		if len(chunks) == 0 {
			return fill(out, pluginapi.HostModelStreamReadResponse{Done: true})
		}
		chunk := chunks[0]
		h.chunks[account] = chunks[1:]
		return fill(out, chunk)
	case pluginabi.MethodHostModelStreamClose:
		h.upstreamClosed = append(h.upstreamClosed, in.(pluginapi.HostModelStreamCloseRequest).StreamID)
		return nil
	case pluginabi.MethodHostStreamEmit:
		if h.emitError {
			return fmt.Errorf("client cancelled")
		}
		h.emitted = append(h.emitted, in.(map[string]any)["payload"].([]byte))
		return nil
	case pluginabi.MethodHostStreamClose:
		h.closed <- in.(map[string]any)["error"].(string)
		return nil
	}
	return fmt.Errorf("unexpected callback %s", method)
}
func setup(t *testing.T) (*Engine, *fakeHost, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	h := &fakeHost{failures: map[string]int{}, chunks: map[string][]pluginapi.HostModelStreamReadResponse{}, streamAccounts: map[string]string{}, closed: make(chan string, 4)}
	for _, a := range []struct {
		id, provider, model string
		priority            int
	}{{"A", "claude", "opus", 1}, {"B", "antigravity", "opus", 1}, {"G", "codex", "sol", 100}} {
		h.files = append(h.files, pluginapi.HostAuthFileEntry{ID: a.id, Type: a.provider, Priority: a.priority})
		h.models = append(h.models, pluginapi.HostAuthModels{ID: a.id, Provider: a.provider, Models: []string{a.model}})
	}
	e := &Engine{Host: h, Now: func() time.Time { return now }}
	e.Configure([]config.Role{{Name: "designer", Enabled: true, Targets: []config.RoleTarget{{Model: "opus", Effort: "high"}, {Model: "sol", Effort: "xhigh"}}}}, true)
	return e, h, &now
}
func request() Request {
	return Request{ExecutorRequest: pluginapi.ExecutorRequest{Model: "designer", SourceFormat: "claude", OriginalRequest: []byte(`{"model":"designer","messages":[{"role":"user","content":"hello"}]}`)}, HostCallbackID: "callback", StreamID: "out"}
}
func callIDs(h *fakeHost) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []string{}
	for _, c := range h.calls {
		out = append(out, c.AuthID)
	}
	return out
}
func TestOrderedModelsAllAccountsAndAutomaticFailback(t *testing.T) {
	e, h, now := setup(t)
	h.failures["A"] = 429
	h.failures["B"] = 503
	if _, err := e.Execute(request()); err != nil {
		t.Fatal(err)
	}
	if got := callIDs(h); !reflect.DeepEqual(got, []string{"A", "B", "G"}) {
		t.Fatal(got)
	}
	if _, err := e.Execute(request()); err != nil {
		t.Fatal(err)
	}
	if got := callIDs(h); !reflect.DeepEqual(got, []string{"A", "B", "G", "G"}) {
		t.Fatal(got)
	}
	*now = now.Add(31 * time.Second)
	delete(h.failures, "A")
	if _, err := e.Execute(request()); err != nil {
		t.Fatal(err)
	}
	if got := callIDs(h); got[len(got)-1] != "A" {
		t.Fatal(got)
	}
	if h.calls[2].Model != "sol(xhigh)" || h.calls[0].Model != "opus(high)" {
		t.Fatal("lost per-target effort")
	}
}
func TestEffortFixedOverrideMappingAndAuto(t *testing.T) {
	for _, tc := range []struct {
		name           string
		override       bool
		incoming, want string
	}{{"fixed", false, "low", "high"}, {"override", true, "low", "low"}, {"mapped", true, "max", "xhigh"}, {"auto", true, "auto", "high"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := request()
			r.OriginalRequest = []byte(`{"model":"designer","messages":[],"output_config":{"effort":"` + tc.incoming + `","format":{"type":"json_schema"}},"thinking":{"type":"disabled"}}`)
			role := config.Role{AllowClientEffortOverride: tc.override}
			target := config.RoleTarget{Model: "opus", Effort: "high", EffortMap: map[string]string{"max": "xhigh"}}
			body, model, effort, err := effectiveBody(r, role, target)
			if err != nil || effort != tc.want || model != "opus("+tc.want+")" {
				t.Fatalf("%s %s %v", model, effort, err)
			}
			if strings.Contains(string(body), "thinking") || !strings.Contains(string(body), "json_schema") {
				t.Fatal(string(body))
			}
		})
	}
	r := request()
	r.Model = "designer(low)"
	_, model, _, _ := effectiveBody(r, config.Role{}, config.RoleTarget{Model: "sol", Effort: "xhigh"})
	if model != "sol(xhigh)" {
		t.Fatal(model)
	}
}
func TestMalformedRequestsAndCancellationDoNotFallback(t *testing.T) {
	for _, code := range []int{400, 422, 499} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			e, h, _ := setup(t)
			h.failures["A"] = code
			_, err := e.Execute(request())
			if status(err) != code || len(callIDs(h)) != 1 {
				t.Fatalf("%v %v", err, callIDs(h))
			}
		})
	}
	e, h, _ := setup(t)
	r := request()
	r.OriginalRequest = []byte(`invalid`)
	_, err := e.Execute(r)
	if status(err) != 400 || len(callIDs(h)) != 0 {
		t.Fatal(err)
	}
}
func TestRestrictionsConflictAndDisabledRole(t *testing.T) {
	e, h, _ := setup(t)
	e.roles[0].Targets[0].AccountIDs = []string{"B"}
	if _, err := e.Execute(request()); err != nil || callIDs(h)[0] != "B" {
		t.Fatalf("%v %v", err, callIDs(h))
	}
	h.models[0].Models = append(h.models[0].Models, "designer")
	_, err := e.Execute(request())
	if status(err) != 409 {
		t.Fatal(err)
	}
	e.Configure(e.roles, false)
	if e.Handles("designer") || len(e.Models().Models) != 0 {
		t.Fatal("shadow mode advertised a role")
	}
}
func TestStreamFallbackOnlyBeforeOutputAndCleanup(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			e, h, _ := setup(t)
			h.failures["B"] = 429
			if committed {
				h.chunks["A"] = append(h.chunks["A"], pluginapi.HostModelStreamReadResponse{Payload: []byte("first")})
			}
			h.chunks["A"] = append(h.chunks["A"], pluginapi.HostModelStreamReadResponse{Error: `{"status_code":503,"message":"busy"}`, Done: true})
			h.chunks["G"] = []pluginapi.HostModelStreamReadResponse{{Payload: []byte("fallback")}, {Done: true}}
			r := request()
			r.Stream = true
			if _, err := e.ExecuteStream(r); err != nil {
				t.Fatal(err)
			}
			select {
			case terminal := <-h.closed:
				ids := callIDs(h)
				if committed {
					if len(ids) != 1 || terminal == "" {
						t.Fatalf("replayed partial stream: %v %s", ids, terminal)
					}
				} else if !reflect.DeepEqual(ids, []string{"A", "B", "G"}) || terminal != "" {
					t.Fatalf("%v %s", ids, terminal)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stream did not finish")
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.upstreamClosed) == 0 {
				t.Fatal("leaked upstream stream")
			}
		})
	}
}
func TestStreamClientDisconnectNeverStartsAnotherTarget(t *testing.T) {
	e, h, _ := setup(t)
	h.emitError = true
	h.chunks["A"] = []pluginapi.HostModelStreamReadResponse{{Payload: []byte("data")}}
	r := request()
	r.Stream = true
	_, err := e.ExecuteStream(r)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("stream not closed")
	}
	if len(callIDs(h)) != 1 {
		t.Fatal(callIDs(h))
	}
}
func TestRoleHeadersDoNotExposeAccountIDs(t *testing.T) {
	h := diagnosticHeaders(http.Header{}, Decision{Role: "designer", Model: "sol", Effort: "xhigh", AccountID: "secret-account"})
	if strings.Contains(fmt.Sprint(h), "secret-account") {
		t.Fatal(h)
	}
}

func TestConfigBackedAccountsAbsentFromFileListingRemainEligible(t *testing.T) {
	e, h, _ := setup(t)
	h.files = nil
	if _, err := e.Execute(request()); err != nil {
		t.Fatal(err)
	}
	if got := callIDs(h); len(got) != 1 || got[0] != "A" {
		t.Fatal(got)
	}
}
func TestCountRoutingUsesNativeModelAndRestrictions(t *testing.T) {
	e, _, _ := setup(t)
	e.roles[0].Targets[0].AccountIDs = []string{"B"}
	req := pluginapi.ModelRouteRequest{RequestedModel: "designer", Body: request().OriginalRequest, Metadata: map[string]any{"request_path": "/v1/messages/count_tokens"}}
	route, err := e.CountRoute(req)
	if err != nil || route.TargetModel != "opus(high)" || route.Target != "antigravity" {
		t.Fatalf("%+v %v", route, err)
	}
	pick, handled := e.CountPick(pluginapi.SchedulerPickRequest{Model: route.TargetModel, Options: pluginapi.SchedulerOptions{Metadata: map[string]any{"request_path": "/v1/messages/count_tokens", "requested_model": "designer"}}, Candidates: []pluginapi.SchedulerAuthCandidate{{ID: "A", Provider: "claude"}, {ID: "B", Provider: "antigravity"}}})
	if !handled || pick.AuthID != "B" {
		t.Fatalf("%+v", pick)
	}
}

func TestRoleSessionNamespaceIsStableAndSeparate(t *testing.T) {
	req := request()
	req.Headers = http.Header{"Session-Id": {"original-thread"}}
	req.Metadata = map[string]any{"caller_scope": "scope"}
	a := roleHeaders(req, "designer")
	b := roleHeaders(req, "designer")
	c := roleHeaders(req, "reviewer")
	if a.Get("X-Claude-Code-Session-Id") != b.Get("X-Claude-Code-Session-Id") || a.Get("X-Claude-Code-Session-Id") == c.Get("X-Claude-Code-Session-Id") {
		t.Fatal("role namespace is unstable")
	}
	if a.Get("Session-Id") != "original-thread" || req.Headers.Get("X-Claude-Code-Session-Id") != "" {
		t.Fatal("mutated original headers")
	}
	if !IsRoleSession("claude:" + a.Get("X-Claude-Code-Session-Id")) {
		t.Fatal("usage would leak into desktop affinity")
	}
}
