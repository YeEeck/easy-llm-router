package logging

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	hub := &Hub{max: 500, file: rotator}
	options := &slog.HandlerOptions{Level: parseLevel(level)}
	return slog.New(slog.NewJSONHandler(hub, options)), hub, nil
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
