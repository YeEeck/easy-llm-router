package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
	"github.com/yeck/easy-llm-router/internal/routing"
)

func TestTransparentCredentialFailover(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		key := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		keys = append(keys, key)
		mu.Unlock()
		if key == "key-a" {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"error":{"type":"GoUsageLimitError"}}`)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"ok":true}`)
	}))
	defer upstream.Close()

	manager := routerTestManager(t, upstream.URL)
	handler := New(Options{Manager: manager, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), ReplayMemoryLimit: 8 << 20, ReplayLimit: 256 << 20, ResponseInspectLimit: 1 << 20, TempDir: t.TempDir()})
	request := httptest.NewRequest(http.MethodPost, "http://router/pools/main/chat/completions?x=1", strings.NewReader(`{"model":"test"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"ok":true}` {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] != "key-a" || keys[1] != "key-b" {
		t.Fatalf("keys = %#v", keys)
	}
	_, state := manager.Snapshot()
	if state.Credentials["a"].Status != domain.StatusExhausted || state.Current["main"] != "b" {
		t.Fatalf("state = %#v", state)
	}
}

func TestTransientRateLimitIsNotRetried(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"error":{"code":"provider_rate_limit_exceeded"}}`)
	}))
	defer upstream.Close()
	manager := routerTestManager(t, upstream.URL)
	handler := New(Options{Manager: manager, ReplayMemoryLimit: 1024, ReplayLimit: 2048, ResponseInspectLimit: 1024, TempDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://router/pools/main/chat", strings.NewReader("{}")))
	if requests != 1 || response.Code != http.StatusTooManyRequests {
		t.Fatalf("requests=%d response=%d", requests, response.Code)
	}
}

func TestExhaustedCurrentCanRecoverAsLastResort(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	manager := routerTestManager(t, upstream.URL)
	for _, id := range []string{"a", "b"} {
		if err := manager.Transition("main", id, domain.ClassExhausted, "quota", time.Time{}, ""); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(Options{Manager: manager, ReplayMemoryLimit: 1024, ReplayLimit: 2048, ResponseInspectLimit: 1024, TempDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://router/pools/main/chat", strings.NewReader("{}")))
	_, state := manager.Snapshot()
	if response.Code != http.StatusOK || state.Credentials["a"].Status != domain.StatusAvailable {
		t.Fatalf("response=%d state=%#v", response.Code, state)
	}
}

func TestLargeFinalErrorBodyIsForwardedCompletely(t *testing.T) {
	largeBody := `{"error":{"type":"GoUsageLimitError"},"padding":"` + strings.Repeat("x", 4096) + `"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, largeBody)
	}))
	defer upstream.Close()
	manager := routerTestManager(t, upstream.URL)
	if err := manager.SetStatus("main", "b", domain.StatusDisabled, "test"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := manager.Snapshot()
	cfg.Services[0].Rules = append([]domain.ResponseRule{{
		Name: "quota-status", Result: domain.ClassExhausted,
		Conditions: []domain.Condition{{StatusMin: http.StatusTooManyRequests, StatusMax: http.StatusTooManyRequests}},
	}}, cfg.Services[0].Rules...)
	if err := manager.ReplaceConfig(cfg, manager.Secrets()); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{Manager: manager, ReplayMemoryLimit: 1024, ReplayLimit: 2048, ResponseInspectLimit: 128, TempDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://router/pools/main/chat", strings.NewReader("{}")))
	if response.Code != http.StatusTooManyRequests || response.Body.String() != largeBody {
		t.Fatalf("response=%d bytes=%d, want %d", response.Code, response.Body.Len(), len(largeBody))
	}
}

func TestStreamErrorUpdatesStateWithoutRetry(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "event: error\ndata: quota-reached\n\n")
	}))
	defer upstream.Close()
	manager := routerTestManager(t, upstream.URL)
	cfg, _ := manager.Snapshot()
	cfg.Services[0].Rules = append([]domain.ResponseRule{{
		Name: "stream-quota", Result: domain.ClassExhausted,
		Conditions: []domain.Condition{{Body: true, Operator: domain.MatchRegex, Value: "quota-reached"}},
	}}, cfg.Services[0].Rules...)
	if err := manager.ReplaceConfig(cfg, manager.Secrets()); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{Manager: manager, ReplayMemoryLimit: 1024, ReplayLimit: 2048, ResponseInspectLimit: 1024, TempDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://router/pools/main/chat", strings.NewReader("{}")))
	_, state := manager.Snapshot()
	if requests != 1 || state.Credentials["a"].Status != domain.StatusExhausted || !strings.Contains(response.Body.String(), "quota-reached") {
		t.Fatalf("requests=%d state=%#v response=%q", requests, state, response.Body.String())
	}
}

func TestRecoveryHintCapturedOnExhaustedTransition(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "2094")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"type":"error","error":{"type":"GoUsageLimitError","message":"5-hour usage limit reached. Resets in 35min."},"metadata":{"limitName":"5 hour"}}`)
	}))
	defer upstream.Close()
	manager := routerTestManager(t, upstream.URL)
	cfg, _ := manager.Snapshot()
	cfg.Services[0].RecoveryHint = domain.RecoveryHintConfig{Source: domain.RecoveryHintFromHeader, Header: "Retry-After", Parse: domain.RecoveryHintAsSeconds}
	cfg.Services[0].QuotaEpoch = domain.QuotaEpochConfig{Source: domain.RecoveryHintFromJSON, JSONPath: "metadata.limitName"}
	if err := manager.ReplaceConfig(cfg, manager.Secrets()); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{Manager: manager, ReplayMemoryLimit: 1024, ReplayLimit: 2048, ResponseInspectLimit: 4096, TempDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	before := time.Now()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://router/pools/main/chat", strings.NewReader("{}")))
	_, state := manager.Snapshot()
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("response code=%d, want %d", response.Code, http.StatusTooManyRequests)
	}
	hint := state.Credentials["a"].RecoveryHint
	if hint.IsZero() {
		t.Fatalf("recovery hint not captured")
	}
	if hint.Before(before.Add(2000*time.Second)) || hint.After(before.Add(2200*time.Second)) {
		t.Fatalf("recovery hint = %v, expected ~now+2094s (before=%v)", hint, before)
	}
	if epoch := state.Credentials["a"].QuotaEpoch; epoch != "5 hour" {
		t.Fatalf("quota epoch = %q, want \"5 hour\"", epoch)
	}
}

func routerTestManager(t *testing.T, baseURL string) *routing.Manager {
	t.Helper()
	rules := []domain.ResponseRule{
		{Name: "quota", Result: domain.ClassExhausted, Conditions: []domain.Condition{{JSONPath: "error.type", Operator: domain.MatchEquals, Value: "GoUsageLimitError"}}},
		{Name: "auth", Result: domain.ClassInvalid, Conditions: []domain.Condition{{StatusMin: 401, StatusMax: 403}}},
	}
	cfg := domain.Config{
		Services:    []domain.Service{{ID: "svc", BaseURL: baseURL + "/v1", AuthHeader: "Authorization", AuthPrefix: "Bearer ", Rules: rules}},
		Credentials: []domain.Credential{{ID: "a", ServiceID: "svc"}, {ID: "b", ServiceID: "svc"}},
		Pools:       []domain.Pool{{Name: "main", CredentialIDs: []string{"a", "b"}, VerifyInterval: 5 * time.Minute}},
	}
	secrets := domain.Secrets{APIKeys: map[string]string{"a": "key-a", "b": "key-b"}}
	state := domain.RuntimeState{Credentials: map[string]domain.CredentialState{"a": {Status: domain.StatusAvailable}, "b": {Status: domain.StatusAvailable}}, Current: map[string]string{"main": "a"}}
	manager, err := routing.New(cfg, secrets, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
