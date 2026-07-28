package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yeck/easy-llm-router/internal/domain"
)

type Request struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func Build(config domain.ProbeConfig) (Request, error) {
	method := http.MethodPost
	path := config.Path
	body := config.Body
	switch config.Protocol {
	case domain.ProbeOpenAIChat:
		if path == "" {
			path = "/chat/completions"
		}
		body = map[string]any{
			"model": config.Model, "stream": false, "max_tokens": 1,
			"messages": []map[string]string{{"role": "user", "content": "Reply OK"}},
		}
	case domain.ProbeOpenAIResponses:
		if path == "" {
			path = "/responses"
		}
		body = map[string]any{
			"model": config.Model, "stream": false, "max_output_tokens": 1, "input": "Reply OK",
		}
	case domain.ProbeAnthropic:
		if path == "" {
			path = "/messages"
		}
		body = map[string]any{
			"model": config.Model, "stream": false, "max_tokens": 1,
			"messages": []map[string]string{{"role": "user", "content": "Reply OK"}},
		}
	case domain.ProbeCustom:
		if config.Method != "" {
			method = strings.ToUpper(config.Method)
		}
		if path == "" {
			return Request{}, fmt.Errorf("custom probe path is required")
		}
	default:
		return Request{}, fmt.Errorf("unsupported probe protocol %q", config.Protocol)
	}
	if config.Protocol != domain.ProbeCustom && strings.TrimSpace(config.Model) == "" {
		return Request{}, fmt.Errorf("probe model is required")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return Request{}, fmt.Errorf("encode probe body: %w", err)
	}
	headers := make(http.Header, len(config.Headers)+1)
	for name, value := range config.Headers {
		headers.Set(name, value)
	}
	headers.Set("Content-Type", "application/json")
	return Request{Method: method, Path: path, Headers: headers, Body: encoded}, nil
}

func (r Request) HTTP(baseURL string) (*http.Request, error) {
	request, err := http.NewRequest(r.Method, strings.TrimRight(baseURL, "/")+"/"+strings.TrimLeft(r.Path, "/"), bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	request.Header = r.Headers.Clone()
	request.ContentLength = int64(len(r.Body))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(r.Body)), nil
	}
	return request, nil
}
