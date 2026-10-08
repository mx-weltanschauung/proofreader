package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
)

// MemoryStorage is an in-memory Storage for tests.
type MemoryStorage struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

// NewMemoryStorage creates an empty in-memory store.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{objects: make(map[string][]byte)}
}

func (m *MemoryStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *MemoryStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("storage: object not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *MemoryStorage) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return fmt.Sprintf("mem://%s?ttl=%s", key, ttl), nil
}

func (m *MemoryStorage) PresignGetAttachment(_ context.Context, key string, ttl time.Duration, disposition string) (string, error) {
	return fmt.Sprintf("mem://%s?ttl=%s&disposition=%s", key, ttl, url.QueryEscape(disposition)), nil
}

func (m *MemoryStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *MemoryStorage) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			delete(m.objects, k)
		}
	}
	return nil
}

func (m *MemoryStorage) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	return fmt.Sprintf("mem://put/%s?ct=%s&ttl=%s", key, contentType, ttl), nil
}

func (m *MemoryStorage) Head(_ context.Context, key string) (int64, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.objects[key]
	if !ok {
		return 0, "", fmt.Errorf("%w: %s", ErrObjectNotFound, key)
	}
	sum := md5.Sum(data)
	return int64(len(data)), hex.EncodeToString(sum[:]), nil
}

// HasPrefix reports whether any stored key starts with prefix.
func (m *MemoryStorage) HasPrefix(_ context.Context, prefix string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			return true, nil
		}
	}
	return false, nil
}
