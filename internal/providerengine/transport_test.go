package providerengine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOAuthBufferedAndStreamUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("browser cookie leaked")
		}
		if r.Header.Get("X-App") != "" {
			t.Error("client X-App header leaked to SDK upstream")
		}
		var b struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		if strings.HasPrefix(b.Model, "ng-") {
			t.Error("internal namespace sent upstream")
		}
		if b.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\""+b.Model+"\",\"content\":[],\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "msg_test", "type": "message", "role": "assistant", "model": b.Model, "content": []map[string]string{{"type": "text", "text": "hello"}}, "stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 11, "output_tokens": 3}})
	}))
	defer upstream.Close()
	s, d := testStore(t)
	a := grantRecord(t, s)
	a.Attributes["base_url"] = upstream.URL
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	e, err := New(d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	models, err := e.Models(ctx, "p")
	if err != nil || len(models) == 0 {
		t.Fatal(err)
	}
	model := models[0]["id"].(string)
	for _, stream := range []bool{false, true} {
		raw, _ := json.Marshal(map[string]any{"model": model, "max_tokens": 64, "messages": []map[string]string{{"role": "user", "content": "hi"}}, "stream": stream})
		req := httptest.NewRequest("POST", "http://upstream.invalid/v1/messages", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nineguard_session=fake")
		req.Header.Set("X-App", "cli")
		resp, err := e.RoundTrip(ctx, "p", "", req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || !strings.Contains(string(body), "hello") {
			t.Fatalf("response %d %s", resp.StatusCode, body)
		}
	}
	req := httptest.NewRequest("POST", "http://upstream.invalid/v1/messages", strings.NewReader(`{"model":"not-available"}`))
	if _, err = e.RoundTrip(ctx, "other-provider", a.ID, req); err == nil {
		t.Fatal("cross-provider account accepted")
	}
}

func TestOAuthExplicitAccountIsPinned(t *testing.T) {
	s, d := testStore(t)
	makeAccount := func(label string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"msg_test","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"`+label+`"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		}))
	}
	one, two := makeAccount("one"), makeAccount("two")
	defer one.Close()
	defer two.Close()
	a := grantRecord(t, s)
	a.Attributes["base_url"] = one.URL
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	b := grantRecord(t, s)
	b.Attributes["base_url"] = two.URL
	if _, err := s.Save(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	e, err := New(d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	models, err := e.Models(ctx, "p")
	if err != nil || len(models) == 0 {
		t.Fatal(err)
	}
	model := models[0]["id"].(string)
	for range 4 {
		raw, _ := json.Marshal(map[string]any{"model": model, "max_tokens": 64, "messages": []map[string]string{{"role": "user", "content": "hi"}}})
		req := httptest.NewRequest("POST", "http://unused/v1/messages", strings.NewReader(string(raw)))
		resp, err := e.RoundTrip(ctx, "p", b.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(body), `"text":"two"`) {
			t.Fatalf("explicit account escaped: %s", body)
		}
	}
}
