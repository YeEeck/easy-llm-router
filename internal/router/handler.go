package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yeck/easy-llm-router/internal/classify"
	"github.com/yeck/easy-llm-router/internal/domain"
	appLog "github.com/yeck/easy-llm-router/internal/logging"
	"github.com/yeck/easy-llm-router/internal/routing"
)

type Handler struct {
	manager      *routing.Manager
	client       *http.Client
	logger       *slog.Logger
	memoryLimit  int64
	replayLimit  int64
	inspectLimit int64
	tempDir      string
	active       atomic.Int64
}

type Options struct {
	Manager              *routing.Manager
	Client               *http.Client
	Logger               *slog.Logger
	ReplayMemoryLimit    int64
	ReplayLimit          int64
	ResponseInspectLimit int64
	TempDir              string
}

func New(options Options) *Handler {
	client := options.Client
	if client == nil {
		client = &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		manager: options.Manager, client: client, logger: logger,
		memoryLimit: options.ReplayMemoryLimit, replayLimit: options.ReplayLimit,
		inspectLimit: options.ResponseInspectLimit, tempDir: options.TempDir,
	}
}

func (h *Handler) ActiveRequests() int64 { return h.active.Load() }

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	h.active.Add(1)
	defer h.active.Add(-1)
	started := time.Now()
	requestID := requestID()
	poolName, suffix, ok := parsePoolPath(request.URL.Path)
	if !ok {
		writeLocalError(writer, http.StatusNotFound, "router_pool_not_found", "expected /pools/{name}/...")
		return
	}
	body, err := CaptureBody(request.Body, h.memoryLimit, h.replayLimit, h.tempDir)
	if err != nil {
		h.logger.Error("capture request body", "request_id", requestID, "pool", poolName, "error", err)
		writeLocalError(writer, http.StatusBadRequest, "router_request_body_error", err.Error())
		return
	}
	defer body.Close()
	if !body.Replayable() {
		h.logger.Info("request replay disabled", "request_id", requestID, "pool", poolName, "bytes", body.Size())
	}
	attempted := map[string]bool{}
	selection, err := h.manager.Start(poolName, attempted)
	if err != nil {
		writeLocalError(writer, http.StatusServiceUnavailable, "router_no_selectable_credential", err.Error())
		return
	}
	for {
		attempted[selection.Credential.ID] = true
		response, err := h.roundTrip(request.Context(), request, suffix, selection, body)
		if err != nil {
			h.logger.Error("upstream request failed", "request_id", requestID, "pool", poolName,
				"credential", selection.Credential.ID, "error", err, "duration_ms", time.Since(started).Milliseconds())
			writeLocalError(writer, http.StatusBadGateway, "router_upstream_error", err.Error())
			return
		}
		if isEventStream(response) {
			h.streamResponse(writer, response, poolName, requestID, selection)
			return
		}
		prefix, complete, readErr := readPrefix(response.Body, h.inspectLimit)
		matchBody := prefix
		if !complete {
			matchBody = nil
		}
		match := classify.Evaluate(selection.Service.Rules, classify.Response{StatusCode: response.StatusCode, Header: response.Header, Body: matchBody})
		if match.Result != domain.ClassSuccess && h.logger.Enabled(request.Context(), slog.LevelDebug) {
			h.logger.Debug("upstream error response", "request_id", requestID, "pool", poolName,
				"credential", selection.Credential.ID, "headers", appLog.SafeHeaders(response.Header),
				"error_body", appLog.ErrorPreview(prefix, 4<<10))
		}
		h.transition(poolName, selection.Credential.ID, match, requestID)
		canRetry := body.Replayable() && !selection.LastResort && (match.Result == domain.ClassExhausted || match.Result == domain.ClassInvalid)
		if canRetry {
			next, nextErr := h.manager.Next(poolName, selection.Credential.ID, attempted)
			if nextErr == nil {
				response.Body.Close()
				h.logger.Info("retry with next credential", "request_id", requestID, "pool", poolName,
					"from", selection.Credential.ID, "to", next.Credential.ID, "reason", match.Result)
				selection = next
				continue
			}
		}
		copyHeaders(writer.Header(), response.Header)
		writer.WriteHeader(response.StatusCode)
		_, _ = writer.Write(prefix)
		if !complete && readErr == nil {
			_, _ = io.Copy(writer, response.Body)
		}
		response.Body.Close()
		h.logger.Info("request complete", "request_id", requestID, "pool", poolName,
			"credential", selection.Credential.ID, "credential_fingerprint", appLog.Fingerprint(selection.APIKey),
			"status", response.StatusCode, "classification", match.Result, "rule", match.RuleName,
			"duration_ms", time.Since(started).Milliseconds())
		return
	}
}

func (h *Handler) roundTrip(ctx context.Context, incoming *http.Request, suffix string, selection routing.Selection, body *ReplayBody) (*http.Response, error) {
	base := selection.Service.BaseURL
	if selection.Credential.BaseURLOverride != "" {
		base = selection.Credential.BaseURLOverride
	}
	target, err := joinURL(base, suffix, incoming.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	reader, err := body.Open()
	if err != nil {
		return nil, err
	}
	outgoing, err := http.NewRequestWithContext(ctx, incoming.Method, target, reader)
	if err != nil {
		reader.Close()
		return nil, err
	}
	outgoing.ContentLength = body.Size()
	outgoing.Header = incoming.Header.Clone()
	removeHopHeaders(outgoing.Header)
	outgoing.Header.Del("Authorization")
	outgoing.Header.Del("X-Api-Key")
	outgoing.Header.Del("Api-Key")
	authHeader := selection.Service.AuthHeader
	if authHeader == "" {
		authHeader = "Authorization"
	}
	outgoing.Header.Set(authHeader, selection.Service.AuthPrefix+selection.APIKey)
	return h.client.Do(outgoing)
}

func (h *Handler) streamResponse(writer http.ResponseWriter, response *http.Response, poolName, requestID string, selection routing.Selection) {
	defer response.Body.Close()
	copyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	flusher, _ := writer.(http.Flusher)
	collected := bytes.NewBuffer(nil)
	buffer := make([]byte, 32*1024)
	for {
		count, err := response.Body.Read(buffer)
		if count > 0 {
			if int64(collected.Len()) < h.inspectLimit {
				remaining := int(h.inspectLimit - int64(collected.Len()))
				if remaining > count {
					remaining = count
				}
				collected.Write(buffer[:remaining])
			}
			_, _ = writer.Write(buffer[:count])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
	match := classify.Evaluate(selection.Service.Rules, classify.Response{StatusCode: response.StatusCode, Header: response.Header, Body: collected.Bytes()})
	h.transition(poolName, selection.Credential.ID, match, requestID)
	h.logger.Info("stream complete", "request_id", requestID, "pool", poolName,
		"credential", selection.Credential.ID, "status", response.StatusCode, "classification", match.Result, "rule", match.RuleName)
}

func (h *Handler) transition(poolName, credentialID string, match classify.Match, requestID string) {
	if err := h.manager.Transition(poolName, credentialID, match.Result, match.RuleName); err != nil {
		h.logger.Error("persist credential transition", "request_id", requestID, "credential", credentialID, "error", err)
	}
}

func parsePoolPath(path string) (string, string, bool) {
	const prefix = "/pools/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	remainder := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(remainder, "/", 2)
	if parts[0] == "" {
		return "", "", false
	}
	suffix := "/"
	if len(parts) == 2 {
		suffix += parts[1]
	}
	return parts[0], suffix, true
}

func joinURL(base, suffix, rawQuery string) (string, error) {
	target, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	target.RawQuery = rawQuery
	return target.String(), nil
}

func readPrefix(reader io.Reader, limit int64) ([]byte, bool, error) {
	if limit < 1 {
		limit = 1
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return data, len(data) <= int(limit), err
	}
	if int64(len(data)) <= limit {
		return data, true, nil
	}
	return data, false, nil
}

func isEventStream(response *http.Response) bool {
	return response.StatusCode >= 200 && response.StatusCode < 300 && strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream")
}

func copyHeaders(destination, source http.Header) {
	for name, values := range source {
		if isHopHeader(name) {
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func removeHopHeaders(header http.Header) {
	for name := range header {
		if isHopHeader(name) {
			header.Del(name)
		}
	}
}

func isHopHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func writeLocalError(writer http.ResponseWriter, status int, code, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"type": code, "message": message}})
}

func requestID() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}

func IsNoCredential(err error) bool { return errors.Is(err, routing.ErrNoCredential) }
