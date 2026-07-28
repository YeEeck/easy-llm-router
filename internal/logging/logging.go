package logging

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Hub struct {
	mu    sync.RWMutex
	lines []string
	max   int
	file  io.Writer
}

func New(path, level string) (*slog.Logger, *Hub, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	rotator := &lumberjack.Logger{Filename: path, MaxSize: 10, MaxBackups: 5, Compress: false}
	hub := NewHub(rotator)
	options := &slog.HandlerOptions{Level: parseLevel(level)}
	return slog.New(slog.NewJSONHandler(hub, options)), hub, nil
}

// NewHub returns a Hub that mirrors log lines to w.
func NewHub(w io.Writer) *Hub {
	return &Hub{max: 500, file: w}
}

func (h *Hub) Write(data []byte) (int, error) {
	written, err := h.file.Write(data)
	line := strings.TrimSpace(string(data))
	if line != "" {
		h.mu.Lock()
		h.lines = append(h.lines, line)
		if len(h.lines) > h.max {
			h.lines = append([]string(nil), h.lines[len(h.lines)-h.max:]...)
		}
		h.mu.Unlock()
	}
	return written, err
}

func (h *Hub) Lines() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]string(nil), h.lines...)
}

func Fingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("sha256:%x", sum[:4])
}

func SafeHeaders(header http.Header) map[string][]string {
	result := make(map[string][]string, len(header))
	for name, values := range header {
		switch strings.ToLower(name) {
		case "authorization", "proxy-authorization", "x-api-key", "api-key", "cookie", "set-cookie":
			continue
		}
		result[name] = append([]string(nil), values...)
	}
	return result
}

func ErrorPreview(body []byte, limit int) string {
	if limit < 0 {
		limit = 0
	}
	if len(body) > limit {
		body = body[:limit]
	}
	preview := string(body)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)("(?:api[_-]?key|authorization|access[_-]?token|refresh[_-]?token)"\s*:\s*")[^"]*`),
		regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/-]+`),
	}
	for _, pattern := range patterns {
		preview = pattern.ReplaceAllString(preview, `${1}[REDACTED]`)
	}
	return preview
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
