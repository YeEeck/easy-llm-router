package probe

import (
	"encoding/json"
	"testing"

	"github.com/yeck/easy-llm-router/internal/domain"
)

func TestBuildOpenAIChat(t *testing.T) {
	request, err := Build(domain.ProbeConfig{Protocol: domain.ProbeOpenAIChat, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Path != "/chat/completions" {
		t.Fatalf("path = %q", request.Path)
	}
	var body map[string]any
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "test-model" || body["stream"] != false {
		t.Fatalf("body = %#v", body)
	}
}
