package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPipelineExecution_OrderAndTransformation(t *testing.T) {
	// Setup 2 HTTP mock plugins
	var executionOrder []string
	p1Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executionOrder = append(executionOrder, "p1")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		req, _ := body["request"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request":      req,
			"tokens_saved": 10,
		})
	}))
	defer p1Srv.Close()

	p2Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executionOrder = append(executionOrder, "p2")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		req, _ := body["request"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request":         req,
			"tokens_overhead": 5,
		})
	}))
	defer p2Srv.Close()

	pluginsList := []Plugin{
		{ID: "p2", Kind: KindHTTP, URL: p2Srv.URL, PipelineOrder: 50, Bypassable: true, FailurePolicy: PolicyOpen},
		{ID: "p1", Kind: KindHTTP, URL: p1Srv.URL, PipelineOrder: 10, Bypassable: true, FailurePolicy: PolicyOpen},
	}
	bindings := []Binding{
		{PluginID: "p1", ScopeType: ScopeGlobal, State: StateOn},
		{PluginID: "p2", ScopeType: ScopeGlobal, State: StateOn},
	}

	executor := NewPipelineExecutor(p1Srv.Client())
	initBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	res, _, err := executor.Execute(context.Background(), pluginsList, bindings, nil, "k1", "Key 1", "gpt-4o", "prov1", false, "", initBody)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if len(executionOrder) != 2 || executionOrder[0] != "p1" || executionOrder[1] != "p2" {
		t.Fatalf("expected order [p1, p2], got %v", executionOrder)
	}
	if res.TokensSaved != 10 {
		t.Errorf("expected 10 tokens saved, got %d", res.TokensSaved)
	}
	if res.TokensOverhead != 5 {
		t.Errorf("expected 5 tokens overhead, got %d", res.TokensOverhead)
	}
}

func TestPipelineExecution_BypassAndIdempotency(t *testing.T) {
	pluginsList := []Plugin{
		{ID: "caveman", Kind: KindBuiltin, PipelineOrder: 10, Bypassable: true, FailurePolicy: PolicyOpen},
	}
	bindings := []Binding{
		{PluginID: "caveman", ScopeType: ScopeGlobal, State: StateOn},
	}
	executor := NewPipelineExecutor(nil)
	initBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	// 1. Bypass requested -> should skip
	res, _, err := executor.Execute(context.Background(), pluginsList, bindings, nil, "k1", "Key 1", "gpt-4o", "prov1", true, "", initBody)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.PluginsSkipped) != 1 || res.PluginsSkipped[0] != "caveman:bypassed" {
		t.Fatalf("expected caveman:bypassed, got %v", res.PluginsSkipped)
	}

	// 2. Incoming header has caveman -> should skip with already_applied
	res2, appliedHdr, err2 := executor.Execute(context.Background(), pluginsList, bindings, nil, "k1", "Key 1", "gpt-4o", "prov1", false, "caveman", initBody)
	if err2 != nil {
		t.Fatalf("err: %v", err2)
	}
	if len(res2.PluginsSkipped) != 1 || res2.PluginsSkipped[0] != "caveman:already_applied" {
		t.Fatalf("expected caveman:already_applied, got %v", res2.PluginsSkipped)
	}
	if appliedHdr != "caveman" {
		t.Errorf("expected applied header caveman, got %s", appliedHdr)
	}
}

func TestPipelineExecution_FailOpenAndClosed(t *testing.T) {
	failingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer failingSrv.Close()

	initBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	// Fail open
	openPlugin := Plugin{ID: "open-p", Kind: KindHTTP, URL: failingSrv.URL, FailurePolicy: PolicyOpen, PipelineOrder: 10}
	openBindings := []Binding{{PluginID: "open-p", ScopeType: ScopeGlobal, State: StateOn}}
	executor := NewPipelineExecutor(failingSrv.Client())

	res, _, err := executor.Execute(context.Background(), []Plugin{openPlugin}, openBindings, nil, "", "", "gpt-4o", "", false, "", initBody)
	if err != nil || res.Rejected {
		t.Fatalf("fail-open should not reject: res=%v, err=%v", res, err)
	}
	if len(res.PluginErrors) != 1 || res.PluginErrors[0] != "open-p" {
		t.Fatalf("expected error logged for open-p, got %v", res.PluginErrors)
	}

	// Fail closed
	closedPlugin := Plugin{ID: "closed-p", Kind: KindHTTP, URL: failingSrv.URL, FailurePolicy: PolicyClosed, PipelineOrder: 10}
	closedBindings := []Binding{{PluginID: "closed-p", ScopeType: ScopeGlobal, State: StateOn}}

	resClosed, _, errClosed := executor.Execute(context.Background(), []Plugin{closedPlugin}, closedBindings, nil, "", "", "gpt-4o", "", false, "", initBody)
	if errClosed != nil {
		t.Fatalf("unexpected execution err: %v", errClosed)
	}
	if !resClosed.Rejected || resClosed.RejectCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 rejection for fail-closed, got %v", resClosed)
	}
}

func TestPipelineExecution_ExplicitReject(t *testing.T) {
	rejectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"action":  "reject",
			"message": "PII detected",
		})
	}))
	defer rejectSrv.Close()

	plugin := Plugin{ID: "pii", Kind: KindHTTP, URL: rejectSrv.URL, FailurePolicy: PolicyOpen, PipelineOrder: 10}
	bindings := []Binding{{PluginID: "pii", ScopeType: ScopeGlobal, State: StateOn}}

	executor := NewPipelineExecutor(rejectSrv.Client())
	initBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	res, _, err := executor.Execute(context.Background(), []Plugin{plugin}, bindings, nil, "", "", "gpt-4o", "", false, "", initBody)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Rejected || res.RejectCode != http.StatusForbidden || !strings.Contains(res.RejectMessage, "PII detected") {
		t.Fatalf("expected 403 rejection with message, got %+v", res)
	}
}
