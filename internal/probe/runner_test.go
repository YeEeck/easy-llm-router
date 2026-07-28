package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
	"github.com/yeck/easy-llm-router/internal/routing"
)

func TestRunnerRestoresCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := domain.Config{
		Services:    []domain.Service{{ID: "svc", BaseURL: server.URL, AuthHeader: "Authorization", AuthPrefix: "Bearer ", Probe: domain.ProbeConfig{Protocol: domain.ProbeOpenAIChat, Model: "test"}}},
		Credentials: []domain.Credential{{ID: "a", ServiceID: "svc"}},
		Pools:       []domain.Pool{{Name: "main", CredentialIDs: []string{"a"}, VerifyInterval: 5 * time.Minute}},
	}
	state := domain.RuntimeState{Credentials: map[string]domain.CredentialState{"a": {Status: domain.StatusExhausted}}, Current: map[string]string{"main": "a"}}
	manager, err := routing.New(cfg, domain.Secrets{APIKeys: map[string]string{"a": "secret"}}, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(manager, nil, nil, 1024)
	result, err := runner.Validate(context.Background(), "main", "a")
	if err != nil || result != domain.ClassSuccess {
		t.Fatalf("result=%q err=%v", result, err)
	}
	_, got := manager.Snapshot()
	if got.Credentials["a"].Status != domain.StatusAvailable {
		t.Fatalf("status=%q", got.Credentials["a"].Status)
	}
}
