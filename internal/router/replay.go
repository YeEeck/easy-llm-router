package router

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type ReplayBody struct {
	memory     []byte
	path       string
	size       int64
	replayable bool
}

func CaptureBody(reader io.Reader, memoryLimit, replayLimit int64, tempDir string) (*ReplayBody, error) {
	body := &ReplayBody{replayable: true}
	if reader == nil {
		return body, nil
	}
	var memory bytes.Buffer
	buffer := make([]byte, 32*1024)
	var file *os.File
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			body.size += int64(count)
			if body.size > replayLimit {
				body.replayable = false
			}
			if file == nil && int64(memory.Len()+count) > memoryLimit {
				if err := os.MkdirAll(tempDir, 0o700); err != nil {
					return nil, err
				}
				var err error
				file, err = os.CreateTemp(tempDir, "request-*")
				if err != nil {
					return nil, err
				}
				body.path = file.Name()
				if err := file.Chmod(0o600); err != nil {
					file.Close()
					body.Close()
					return nil, err
				}
				if _, err := file.Write(memory.Bytes()); err != nil {
					file.Close()
					body.Close()
					return nil, err
				}
			}
			if file != nil {
				if _, err := file.Write(buffer[:count]); err != nil {
					file.Close()
					body.Close()
					return nil, err
				}
			} else {
				memory.Write(buffer[:count])
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				if file != nil {
					file.Close()
				}
				body.Close()
				return nil, readErr
			}
			break
		}
	}
	if file != nil {
		if err := file.Sync(); err != nil {
			file.Close()
			body.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			body.Close()
			return nil, err
		}
	} else {
		body.memory = append([]byte(nil), memory.Bytes()...)
	}
	return body, nil
}

func (b *ReplayBody) Open() (io.ReadCloser, error) {
	if b.path != "" {
		return os.Open(b.path)
	}
	return io.NopCloser(bytes.NewReader(b.memory)), nil
}

func (b *ReplayBody) Size() int64      { return b.size }
func (b *ReplayBody) Replayable() bool { return b.replayable }
func (b *ReplayBody) Close() error {
	if b.path == "" {
		return nil
	}
	return os.Remove(b.path)
}

func CleanupTemp(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != "" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale request body: %w", err)
		}
	}
	return nil
}
