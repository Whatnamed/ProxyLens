package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/client"
	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/queue"
)

func TestReadOnlyEnforcementInSourceCode(t *testing.T) {
	forbiddenPatterns := []string{
		"PATCH /configs",
		"PUT /configs",
		"POST /restart",
		"PUT /proxies",
		"DELETE /connections",
	}

	err := filepath.Walk("../pkg", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pattern := range forbiddenPatterns {
			if strings.Contains(string(content), pattern) {
				return fmt.Errorf("forbidden mutation endpoint '%s' found in production code: %s", pattern, path)
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Read-only enforcement test failed: %v", err)
	}
}

func TestQueueOverloadProtection(t *testing.T) {
	overloadCalled := false
	q := queue.NewBoundedQueue[int](2, func() {
		overloadCalled = true
	})

	// Push 2 items (fill capacity)
	if err := q.Push(1); err != nil {
		t.Fatalf("Failed to push 1: %v", err)
	}
	if err := q.Push(2); err != nil {
		t.Fatalf("Failed to push 2: %v", err)
	}

	// Push 3rd item -> must fail and trigger overload callback
	err := q.Push(3)
	if err != queue.ErrQueueFull {
		t.Fatalf("Expected ErrQueueFull, got %v", err)
	}
	if !overloadCalled {
		t.Errorf("Expected overload callback to be triggered")
	}

	metrics := q.GetMetrics()
	if metrics.OverloadsCount != 1 || metrics.CurrentDepth != 2 {
		t.Errorf("Unexpected queue metrics: %+v", metrics)
	}
}

func TestMockControllerClientVersionAndPreflight(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"meta":    true,
				"version": "1.10.0",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{
		ControllerURL: server.URL,
	}
	c := client.NewControllerClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	v, err := c.CheckVersion(ctx)
	if err != nil {
		t.Fatalf("CheckVersion failed: %v", err)
	}
	if v.Version != "1.10.0" || !v.Meta {
		t.Errorf("Unexpected version info: %+v", v)
	}
}
