package server

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"
)

func startTestServer(t *testing.T, opts ...Option) (*Server, string, context.CancelFunc) {
	t.Helper()

	opts = append([]Option{WithPort(":0")}, opts...)
	srv, err := NewServer(opts...)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() { _ = srv.Start(ctx) }()

	<-srv.Ready()

	base := fmt.Sprintf("http://localhost%s", srv.httpServer.Addr)

	return srv, base, cancel
}

func TestServer_StartAndStop(t *testing.T) {
	srv, err := NewServer(
		WithWorkerPoolSize(2),
		WithDBPoolSize(2),
		WithCacheInterval(1*time.Second),
	)
	if err != nil {
		t.Fatalf("NewServer() returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	<-srv.Ready()
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Stop(shutdownCtx); err != nil {
		t.Errorf("Stop() returned error: %v", err)
	}
}

func TestServer_SuddenShutdown(t *testing.T) {
	srv, base, cancel := startTestServer(t,
		WithWorkerPoolSize(4),
		WithDBPoolSize(2),
		WithCacheInterval(10*time.Second),
		WithRequestTimeout(2*time.Second),
	)

	var wg sync.WaitGroup
	results := make(chan int, 100)
	client := &http.Client{Timeout: 5 * time.Second}

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(base + "/")
			if err != nil {
				return
			}
			defer resp.Body.Close()
			results <- resp.StatusCode
		}()
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Stop(shutdownCtx)

	wg.Wait()
	close(results)

	completed := 0
	for code := range results {
		if code == http.StatusOK {
			completed++
		}
	}

	if completed == 0 {
		t.Error("expected at least some requests to complete before shutdown")
	}
	t.Logf("completed %d/100 requests before shutdown", completed)
}

func TestServer_NoGoroutineLeaks(t *testing.T) {
	baseline := runtime.NumGoroutine()

	srv, err := NewServer(
		WithWorkerPoolSize(4),
		WithDBPoolSize(3),
		WithCacheInterval(1*time.Second),
	)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() { _ = srv.Start(ctx) }()

	<-srv.Ready()
	time.Sleep(200 * time.Millisecond)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Stop(shutdownCtx)

	time.Sleep(500 * time.Millisecond)

	after := runtime.NumGoroutine()
	if after > baseline+2 {
		t.Errorf("goroutine leak: before=%d after=%d (delta=%d)", baseline, after, after-baseline)
	}
}

func TestServer_ShutdownTimeout(t *testing.T) {
	srv, base, cancel := startTestServer(t,
		WithWorkerPoolSize(2),
		WithDBPoolSize(1),
		WithCacheInterval(10*time.Second),
		WithRequestTimeout(30*time.Second),
	)

	go func() {
		client := &http.Client{Timeout: 25 * time.Second}
		_, _ = client.Get(base + "/slow")
	}()
	time.Sleep(100 * time.Millisecond)

	cancel()

	shutdownDeadline := 2 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownDeadline)
	defer shutdownCancel()

	start := time.Now()
	err := srv.Stop(shutdownCtx)
	elapsed := time.Since(start)

	if elapsed > shutdownDeadline+1*time.Second {
		t.Errorf("Stop() took %v, expected to finish within ~%v", elapsed, shutdownDeadline)
	}

	if err == nil {
		t.Log("Stop() returned nil; consider returning an error when shutdown times out")
	}
}

func TestServer_ShutdownOrder(t *testing.T) {
	srv, _, cancel := startTestServer(t,
		WithWorkerPoolSize(2),
		WithDBPoolSize(1),
		WithCacheInterval(10*time.Second),
	)

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Stop(shutdownCtx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}

	expected := []string{"http", "workers", "warmer", "db"}
	order := srv.ShutdownOrder()

	if len(order) != len(expected) {
		t.Fatalf("shutdown order length: got %d, want %d (%v)", len(order), len(expected), order)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Errorf("shutdown step %d: got %q, want %q (full order: %v)", i, order[i], expected[i], order)
		}
	}
}
