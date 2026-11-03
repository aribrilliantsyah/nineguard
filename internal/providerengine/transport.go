package providerengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy"
)

func (e *Engine) RoundTrip(ctx context.Context, providerID, accountID string, req *http.Request) (*http.Response, error) {
	accounts, err := e.store.accounts(ctx, providerID)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, account := range accounts {
		if accountID != "" && account.ID != accountID {
			continue
		}
		a, ok := e.manager.GetByID(account.ID)
		if ok && !a.Disabled {
			candidates = append(candidates, a.ID)
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("provider has no connected OAuth account; connect an account in Providers")
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, errors.New("invalid request body")
	}
	var model string
	if err = json.Unmarshal(body["model"], &model); err != nil {
		return nil, errors.New("model is required")
	}
	selected := ""
	internalModel := ""
	for _, id := range candidates {
		a, ok := e.manager.GetByID(id)
		if !ok || a == nil || a.Disabled {
			continue
		}
		candidate := a.Prefix + "/" + model
		if cliproxy.GlobalModelRegistry().ClientSupportsModel(id, candidate) {
			selected, internalModel = id, candidate
			break
		}
	}
	if selected == "" {
		return nil, errors.New("model is not available to the connected accounts of this provider")
	}
	body["model"], _ = json.Marshal(internalModel)
	raw, err = json.Marshal(body)
	if err != nil {
		return nil, err
	}
	out := req.Clone(handlers.WithPinnedAuthID(ctx, selected))
	out.URL.Scheme = "http"
	out.URL.Host = "127.0.0.1"
	out.Host = "127.0.0.1"
	out.RemoteAddr = "127.0.0.1:1"
	out.RequestURI = ""
	out.Body = io.NopCloser(bytes.NewReader(raw))
	out.ContentLength = int64(len(raw))
	out.Header = out.Header.Clone()
	for _, key := range []string{"Authorization", "X-Api-Key", "Cookie", "Accept-Encoding", sessionHeader, "X-NineGuard-Hop", "X-NineGuard-Trace", "X-NineGuard-Plugins-Applied", "X-App"} {
		out.Header.Del(key)
	}
	out.Header.Set("Authorization", "Bearer "+e.secret)
	out.Header.Del("Content-Length")
	// Pipe rather than buffering so SDK SSE delivery keeps streaming and cancellation backpressure.
	reader, writer := io.Pipe()
	headersReady := make(chan struct{})
	rw := &pipeWriter{header: make(http.Header), body: writer, ready: headersReady, status: 200}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				rw.open()
				writer.CloseWithError(fmt.Errorf("OAuth engine request failed"))
			}
		}()
		e.router.ServeHTTP(rw, out)
		rw.open()
		writer.Close()
	}()
	select {
	case <-ctx.Done():
		reader.CloseWithError(ctx.Err())
		return nil, ctx.Err()
	case <-headersReady:
	}
	return &http.Response{StatusCode: rw.status, Header: rw.header.Clone(), Body: reader, Request: req}, nil
}

type pipeWriter struct {
	header http.Header
	body   *io.PipeWriter
	ready  chan struct{}
	opened bool
	status int
}

func (w *pipeWriter) Header() http.Header { return w.header }
func (w *pipeWriter) open() {
	if !w.opened {
		w.opened = true
		close(w.ready)
	}
}
func (w *pipeWriter) WriteHeader(status int) {
	if w.opened {
		return
	}
	w.status = status
	w.open()
}
func (w *pipeWriter) Write(b []byte) (int, error) { w.open(); return w.body.Write(b) }
func (w *pipeWriter) Flush()                      { w.open() }
