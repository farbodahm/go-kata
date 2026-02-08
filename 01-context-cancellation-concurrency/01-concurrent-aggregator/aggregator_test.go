package aggregator

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// mockService is a configurable mock that implements the Service interface.
type mockService struct {
	result string
	err    error
	delay  time.Duration
}

func (m *mockService) Fetch(ctx context.Context, userID int) (string, error) {
	select {
	case <-time.After(m.delay):
		return m.result, m.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestAggregate_HappyPath(t *testing.T) {
	profileSvc := &mockService{result: "Alice"}
	orderSvc := &mockService{result: "Orders: 5"}

	agg := New(profileSvc, orderSvc, WithTimeout(2*time.Second))

	got, err := agg.Aggregate(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "User: Alice | Orders: 5"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAggregate_SlowPoke(t *testing.T) {
	// Set aggregator timeout to 1s.
	// Mock one service to take 2s.
	// Expected: context deadline exceeded after ~1s, NOT after 2s.
	profileSvc := &mockService{result: "Alice"}
	orderSvc := &mockService{result: "Orders: 5", delay: 2 * time.Second}

	agg := New(profileSvc, orderSvc, WithTimeout(1*time.Second))

	start := time.Now()
	_, err := agg.Aggregate(context.Background(), 1)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("took too long: %v (should be ~1s)", elapsed)
	}
}

func TestAggregate_DominoEffect(t *testing.T) {
	// Profile service fails immediately.
	// Order service takes 10 seconds.
	// Expected: returns error immediately, NOT after 10s.
	profileSvc := &mockService{err: errors.New("profile service down")}
	orderSvc := &mockService{result: "Orders: 5", delay: 10 * time.Second}

	agg := New(profileSvc, orderSvc,
		WithTimeout(15*time.Second),
		WithLogger(slog.Default()),
	)

	start := time.Now()
	_, err := agg.Aggregate(context.Background(), 1)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if elapsed > 1*time.Second {
		t.Errorf("did not fail fast: took %v (should be immediate)", elapsed)
	}
}

func TestAggregate_WithFunctionalOptions(t *testing.T) {
	profileSvc := &mockService{result: "Alice"}
	orderSvc := &mockService{result: "Orders: 5"}

	logger := slog.Default()
	agg := New(profileSvc, orderSvc,
		WithTimeout(3*time.Second),
		WithLogger(logger),
	)

	if agg.timeout != 3*time.Second {
		t.Errorf("timeout not set: got %v, want %v", agg.timeout, 3*time.Second)
	}
	if agg.logger != logger {
		t.Error("logger not set correctly")
	}
}
