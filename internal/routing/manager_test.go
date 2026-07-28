package routing

import (
	"testing"
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	cfg := domain.Config{
		Services: []domain.Service{{ID: "svc", BaseURL: "https://example.com"}},
		Credentials: []domain.Credential{
			{ID: "a", ServiceID: "svc"}, {ID: "b", ServiceID: "svc"}, {ID: "c", ServiceID: "svc"},
		},
		Pools: []domain.Pool{{Name: "main", CredentialIDs: []string{"a", "b", "c"}, VerifyInterval: 5 * time.Minute}},
	}
	secrets := domain.Secrets{APIKeys: map[string]string{"a": "a-key", "b": "b-key", "c": "c-key"}}
	state := domain.RuntimeState{
		Credentials: map[string]domain.CredentialState{
			"a": {Status: domain.StatusAvailable}, "b": {Status: domain.StatusAvailable}, "c": {Status: domain.StatusAvailable},
		},
		Current: map[string]string{"main": "a"},
	}
	manager, err := New(cfg, secrets, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestStickyFailover(t *testing.T) {
	manager := testManager(t)
	first, err := manager.Start("main", map[string]bool{})
	if err != nil || first.Credential.ID != "a" {
		t.Fatalf("first = %#v, %v", first, err)
	}
	if err := manager.Transition("main", "a", domain.ClassExhausted, "quota"); err != nil {
		t.Fatal(err)
	}
	next, err := manager.Next("main", "a", map[string]bool{"a": true})
	if err != nil || next.Credential.ID != "b" {
		t.Fatalf("next = %#v, %v", next, err)
	}
	again, err := manager.Start("main", map[string]bool{})
	if err != nil || again.Credential.ID != "b" {
		t.Fatalf("sticky = %#v, %v", again, err)
	}
}

func TestExhaustedCurrentIsLastResort(t *testing.T) {
	manager := testManager(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := manager.Transition("main", id, domain.ClassExhausted, "quota"); err != nil {
			t.Fatal(err)
		}
	}
	selection, err := manager.Start("main", map[string]bool{})
	if err != nil || selection.Credential.ID != "a" || !selection.LastResort {
		t.Fatalf("selection = %#v, %v", selection, err)
	}
}

func TestDisabledIsNeverLastResort(t *testing.T) {
	manager := testManager(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := manager.SetStatus("main", id, domain.StatusDisabled, "user"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.Start("main", map[string]bool{}); err == nil {
		t.Fatal("expected no credential")
	}
}
