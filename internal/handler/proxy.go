package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"gollama/engine"
	"gollama/internal/config"
	"gollama/internal/queue"
)

type ProxyHandler struct {
	cfg     *config.Config
	engine  *engine.Engine
	sched   *queue.Scheduler
	client  *http.Client
	baseURL *url.URL
}

func NewProxyHandler(cfg *config.Config, eng *engine.Engine, sched *queue.Scheduler) (*ProxyHandler, error) {
	baseURL, err := url.Parse(cfg.Engine.BaseURL)
	if err != nil {
		return nil, err
	}
	return &ProxyHandler{
		cfg:     cfg,
		engine:  eng,
		sched:   sched,
		client:  &http.Client{},
		baseURL: baseURL,
	}, nil
}

func (h *ProxyHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("GET /healthz", h.handleHealth)
}

func (h *ProxyHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if h.engine.IsReady() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "engine_not_ready"})
}

func (h *ProxyHandler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if err := h.sched.Acquire(r.Context()); err != nil {
		slog.Warn("queue acquire failed", slog.Any("error", err))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "queue timeout"})
		return
	}
	defer h.sched.Release()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	_ = r.Body.Close()

	stream := extractStreamFlag(bodyBytes)

	upstreamURL := *h.baseURL
	upstreamURL.Path = r.URL.Path
	upstreamURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream request failed"})
		return
	}

	copyHeaders(req.Header, r.Header)

	resp, err := h.client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream unavailable"})
		return
	}
	defer resp.Body.Close()

	isSSE := stream || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	if isSSE {
		copySSEHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		streamResponse(w, resp)
		return
	}

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func extractStreamFlag(body []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.Stream
}

func copyHeaders(dst, src http.Header) {
	for k, v := range src {
		if strings.EqualFold(k, "Host") {
			continue
		}
		for _, vv := range v {
			dst.Add(k, vv)
		}
	}
}

func copySSEHeaders(dst, src http.Header) {
	for k := range dst {
		dst.Del(k)
	}
	for k, v := range src {
		for _, vv := range v {
			dst.Add(k, vv)
		}
	}
	dst.Set("Content-Type", "text/event-stream")
	dst.Set("Cache-Control", "no-cache")
	dst.Set("Connection", "keep-alive")
}

func streamResponse(w http.ResponseWriter, resp *http.Response) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
