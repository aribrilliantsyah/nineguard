package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nineguard/internal/plugins/builtin"
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

	res, _, err := executor.Execute(context.Background(), pluginsList, bindings, nil, "k1", "Key 1", "gpt-4o", "prov1", "http", "chat", false, "", initBody)
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
	res, _, err := executor.Execute(context.Background(), pluginsList, bindings, nil, "k1", "Key 1", "gpt-4o", "prov1", "http", "chat", true, "", initBody)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.PluginsSkipped) != 1 || res.PluginsSkipped[0] != "caveman:bypassed" {
		t.Fatalf("expected caveman:bypassed, got %v", res.PluginsSkipped)
	}

	// 2. Incoming header has caveman -> should skip with already_applied
	res2, appliedHdr, err2 := executor.Execute(context.Background(), pluginsList, bindings, nil, "k1", "Key 1", "gpt-4o", "prov1", "http", "chat", false, "caveman", initBody)
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

	res, _, err := executor.Execute(context.Background(), []Plugin{openPlugin}, openBindings, nil, "", "", "gpt-4o", "", "http", "chat", false, "", initBody)
	if err != nil || res.Rejected {
		t.Fatalf("fail-open should not reject: res=%v, err=%v", res, err)
	}
	if len(res.PluginErrors) != 1 || res.PluginErrors[0] != "open-p" {
		t.Fatalf("expected error logged for open-p, got %v", res.PluginErrors)
	}

	// Fail closed
	closedPlugin := Plugin{ID: "closed-p", Kind: KindHTTP, URL: failingSrv.URL, FailurePolicy: PolicyClosed, PipelineOrder: 10}
	closedBindings := []Binding{{PluginID: "closed-p", ScopeType: ScopeGlobal, State: StateOn}}

	resClosed, _, errClosed := executor.Execute(context.Background(), []Plugin{closedPlugin}, closedBindings, nil, "", "", "gpt-4o", "", "http", "chat", false, "", initBody)
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

	res, _, err := executor.Execute(context.Background(), []Plugin{plugin}, bindings, nil, "", "", "gpt-4o", "", "http", "chat", false, "", initBody)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Rejected || res.RejectCode != http.StatusForbidden || !strings.Contains(res.RejectMessage, "PII detected") {
		t.Fatalf("expected 403 rejection with message, got %+v", res)
	}
}

func TestPipelineExecution_MessagesFormat(t *testing.T) {
	var httpReceivedFormat string
	httpPluginSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		ctxMap, _ := payload["context"].(map[string]any)
		httpReceivedFormat, _ = ctxMap["api_format"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request": payload["request"],
		})
	}))
	defer httpPluginSrv.Close()

	pluginsList := []Plugin{
		{ID: "caveman", Kind: KindBuiltin, PipelineOrder: 10, Bypassable: true, FailurePolicy: PolicyOpen},
		{ID: "headroom", Kind: KindBuiltin, PipelineOrder: 20, Bypassable: true, FailurePolicy: PolicyOpen},
		{ID: "custom-http", Kind: KindHTTP, URL: httpPluginSrv.URL, PipelineOrder: 30, Bypassable: true, FailurePolicy: PolicyOpen},
	}
	bindings := []Binding{
		{PluginID: "caveman", ScopeType: ScopeGlobal, State: StateOn},
		{PluginID: "headroom", ScopeType: ScopeGlobal, State: StateOn},
		{PluginID: "custom-http", ScopeType: ScopeGlobal, State: StateOn},
	}

	executor := NewPipelineExecutor(httpPluginSrv.Client())
	initBody := []byte(`{
		"model": "claude-3-7-sonnet",
		"system": "Original system instructions.",
		"messages": [
			{"role": "user", "content": "Hello Claude"}
		]
	}`)

	res, _, err := executor.Execute(context.Background(), pluginsList, bindings, nil, "k1", "Key 1", "claude-3-7-sonnet", "p-anthropic", "http", "messages", false, "", initBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Caveman was applied and injected into system
	var outBody map[string]any
	if err := json.Unmarshal(res.Body, &outBody); err != nil {
		t.Fatalf("failed to unmarshal output body: %v", err)
	}
	sysStr, ok := outBody["system"].(string)
	if !ok || !strings.Contains(sysStr, "[nineguard:caveman]") {
		t.Errorf("expected Caveman injected into system, got %v", outBody["system"])
	}

	// 2. Headroom was skipped as unsupported_format
	foundHeadroomSkip := false
	for _, s := range res.PluginsSkipped {
		if s == "headroom:unsupported_format" {
			foundHeadroomSkip = true
			break
		}
	}
	if !foundHeadroomSkip {
		t.Errorf("expected headroom:unsupported_format in PluginsSkipped, got %v", res.PluginsSkipped)
	}

	// 3. HTTP Plugin received api_format: "messages"
	if httpReceivedFormat != "messages" {
		t.Errorf("expected HTTP plugin to receive api_format='messages', got %q", httpReceivedFormat)
	}
}

func TestPipelineExecution_BillionContext_ChatAndMessages(t *testing.T) {
	builtin.ResetBillionContextHealthCache()
	mockBili := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__bili/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockBili.Close()

	plg := Plugin{
		ID:            "billion-context",
		Kind:          KindBuiltin,
		PipelineOrder: 15,
		FailurePolicy: PolicyOpen,
	}
	binding := Binding{
		PluginID:  "billion-context",
		ScopeType: ScopeGlobal,
		State:     StateOn,
		Settings:  fmt.Sprintf(`{"url":"%s"}`, mockBili.URL),
	}
	executor := NewPipelineExecutor(mockBili.Client())

	// Test chat format
	initBodyChat := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	resChat, _, err := executor.Execute(context.Background(), []Plugin{plg}, []Binding{binding}, nil, "k1", "Key 1", "gpt-4o", "p1", "http", "chat", false, "", initBodyChat)
	if err != nil {
		t.Fatalf("chat format error: %v", err)
	}
	if resChat.UpstreamProxyURL != mockBili.URL {
		t.Fatalf("expected UpstreamProxyURL %s, got %s", mockBili.URL, resChat.UpstreamProxyURL)
	}
	if len(resChat.PluginsApplied) != 1 || resChat.PluginsApplied[0] != "billion-context" {
		t.Fatalf("expected plugins applied [billion-context], got %v", resChat.PluginsApplied)
	}

	// Test messages format
	initBodyMsg := []byte(`{"model":"claude-3-7-sonnet","messages":[{"role":"user","content":"hi"}]}`)
	resMsg, _, err := executor.Execute(context.Background(), []Plugin{plg}, []Binding{binding}, nil, "k1", "Key 1", "claude-3-7-sonnet", "p-anthropic", "http", "messages", false, "", initBodyMsg)
	if err != nil {
		t.Fatalf("messages format error: %v", err)
	}
	if resMsg.UpstreamProxyURL != mockBili.URL {
		t.Fatalf("expected UpstreamProxyURL %s, got %s", mockBili.URL, resMsg.UpstreamProxyURL)
	}
	if len(resMsg.PluginsApplied) != 1 || resMsg.PluginsApplied[0] != "billion-context" {
		t.Fatalf("expected plugins applied [billion-context], got %v", resMsg.PluginsApplied)
	}
}

func TestPipelineExecution_BillionContext_CliproxyBackend(t *testing.T) {
	builtin.ResetBillionContextHealthCache()
	plg := Plugin{
		ID:            "billion-context",
		Kind:          KindBuiltin,
		PipelineOrder: 15,
		FailurePolicy: PolicyOpen,
	}
	binding := Binding{
		PluginID:  "billion-context",
		ScopeType: ScopeGlobal,
		State:     StateOn,
		Settings:  `{"url":"http://127.0.0.1:8788"}`,
	}
	executor := NewPipelineExecutor(nil)
	initBody := []byte(`{"model":"claude-3-7-sonnet","messages":[{"role":"user","content":"hi"}]}`)

	res, _, err := executor.Execute(context.Background(), []Plugin{plg}, []Binding{binding}, nil, "k1", "Key 1", "claude-3-7-sonnet", "p-oauth", "cliproxy", "messages", false, "", initBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UpstreamProxyURL != "" {
		t.Fatalf("expected empty UpstreamProxyURL for cliproxy, got %s", res.UpstreamProxyURL)
	}
	if len(res.PluginsSkipped) != 1 || res.PluginsSkipped[0] != "billion-context:unsupported_backend" {
		t.Fatalf("expected billion-context:unsupported_backend in PluginsSkipped, got %v", res.PluginsSkipped)
	}
}

func TestPipelineExecution_BillionContext_FailurePolicy(t *testing.T) {
	builtin.ResetBillionContextHealthCache()
	failingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failingSrv.Close()

	initBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	// Fail open: continues direct
	openPlg := Plugin{ID: "billion-context", Kind: KindBuiltin, PipelineOrder: 15, FailurePolicy: PolicyOpen}
	openBinding := Binding{PluginID: "billion-context", ScopeType: ScopeGlobal, State: StateOn, Settings: fmt.Sprintf(`{"url":"%s"}`, failingSrv.URL)}
	executor := NewPipelineExecutor(failingSrv.Client())

	resOpen, _, errOpen := executor.Execute(context.Background(), []Plugin{openPlg}, []Binding{openBinding}, nil, "k1", "Key 1", "gpt-4o", "p1", "http", "chat", false, "", initBody)
	if errOpen != nil || resOpen.Rejected {
		t.Fatalf("fail-open should not reject: res=%v, err=%v", resOpen, errOpen)
	}
	if resOpen.UpstreamProxyURL != "" {
		t.Fatalf("expected empty UpstreamProxyURL on fail-open, got %s", resOpen.UpstreamProxyURL)
	}
	if len(resOpen.PluginErrors) != 1 || resOpen.PluginErrors[0] != "billion-context" {
		t.Fatalf("expected billion-context in PluginErrors, got %v", resOpen.PluginErrors)
	}

	// Fail closed: rejects 503
	builtin.ResetBillionContextHealthCache()
	closedPlg := Plugin{ID: "billion-context", Kind: KindBuiltin, PipelineOrder: 15, FailurePolicy: PolicyClosed}
	closedBinding := Binding{PluginID: "billion-context", ScopeType: ScopeGlobal, State: StateOn, Settings: fmt.Sprintf(`{"url":"%s"}`, failingSrv.URL)}

	resClosed, _, errClosed := executor.Execute(context.Background(), []Plugin{closedPlg}, []Binding{closedBinding}, nil, "k1", "Key 1", "gpt-4o", "p1", "http", "chat", false, "", initBody)
	if errClosed != nil {
		t.Fatalf("unexpected execution err: %v", errClosed)
	}
	if !resClosed.Rejected || resClosed.RejectCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 rejection for fail-closed, got %v", resClosed)
	}
}

