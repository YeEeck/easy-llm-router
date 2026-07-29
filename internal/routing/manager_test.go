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
	if err := manager.Transition("main", "a", domain.ClassExhausted, "quota", time.Time{}, ""); err != nil {
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
		if err := manager.Transition("main", id, domain.ClassExhausted, "quota", time.Time{}, ""); err != nil {
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

func TestScheduleNextVerificationPreservesReason(t *testing.T) {
	manager := testManager(t)
	if err := manager.Transition("main", "a", domain.ClassExhausted, "quota-root-type", time.Time{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := manager.ScheduleNextVerification("main", "a", "inconclusive: default-inconclusive"); err != nil {
		t.Fatal(err)
	}
	_, state := manager.Snapshot()
	if got := state.Credentials["a"].Reason; got != "quota-root-type" {
		t.Fatalf("reason = %q, want quota-root-type (probe must not overwrite trigger reason)", got)
	}
	if got := state.Credentials["a"].LastValidation; got != "inconclusive: default-inconclusive" {
		t.Fatalf("last_validation = %q, want inconclusive note", got)
	}
}

func TestTransitionRecoveryHintAttachedOnExhausted(t *testing.T) {
	manager := testManager(t)
	parsed := time.Date(2026, 7, 29, 12, 21, 0, 0, time.UTC)
	if err := manager.Transition("main", "a", domain.ClassExhausted, "quota", parsed, "5-hour"); err != nil {
		t.Fatal(err)
	}
	_, state := manager.Snapshot()
	if got := state.Credentials["a"].RecoveryHint; !got.Equal(parsed) {
		t.Fatalf("recovery_hint = %v, want %v", got, parsed)
	}
	if got := state.Credentials["a"].QuotaEpoch; got != "5-hour" {
		t.Fatalf("quota_epoch = %q, want 5-hour", got)
	}
}

func TestTransitionRecoveryHintClearedOnSuccess(t *testing.T) {
	manager := testManager(t)
	parsed := time.Date(2026, 7, 29, 12, 21, 0, 0, time.UTC)
	if err := manager.Transition("main", "a", domain.ClassExhausted, "quota", parsed, "5-hour"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Transition("main", "a", domain.ClassSuccess, "ok", time.Time{}, ""); err != nil {
		t.Fatal(err)
	}
	_, state := manager.Snapshot()
	if got := state.Credentials["a"].RecoveryHint; !got.IsZero() {
		t.Fatalf("recovery_hint = %v, want zero on success transition", got)
	}
	if got := state.Credentials["a"].LastValidation; got != "" {
		t.Fatalf("last_validation = %q, want cleared on success", got)
	}
}
