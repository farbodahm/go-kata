package aggregator

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

// Service represents a microservice that can be fetched concurrently.
// Both ProfileService and OrderService should implement this interface.
type Service interface {
	Fetch(ctx context.Context, userID int) (string, error)
}

// Option is a functional option for configuring the UserAggregator.
type Option func(*UserAggregator)

// WithTimeout sets the timeout for the aggregation operation.
func WithTimeout(d time.Duration) Option {
	return func(a *UserAggregator) {
		a.timeout = d
	}
}

// WithLogger sets the structured logger for the aggregator.
func WithLogger(l *slog.Logger) Option {
	return func(a *UserAggregator) {
		a.logger = l
	}
}

// UserAggregator fetches data from multiple services concurrently
// and combines the results.
type UserAggregator struct {
	profileSvc Service
	orderSvc   Service
	timeout    time.Duration
	logger     *slog.Logger
}

// New creates a new UserAggregator with the given services and options.
func New(profileSvc, orderSvc Service, opts ...Option) *UserAggregator {
	a := &UserAggregator{
		profileSvc: profileSvc,
		orderSvc:   orderSvc,
		timeout:    5 * time.Second,
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Aggregate fetches data from ProfileService and OrderService concurrently
// using errgroup with context cancellation, and returns the combined result.
//
// Expected result format: "User: Alice | Orders: 5"
func (a *UserAggregator) Aggregate(ctx context.Context, id int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	errgroup, gctx := errgroup.WithContext(ctx)

	profileCh := make(chan string, 1)
	ordersCh := make(chan string, 1)

	errgroup.Go(func() error {
		profile, err := a.profileSvc.Fetch(gctx, id)
		if err != nil {
			a.logger.Error("failed to fetch profile", "error", err)
			return err
		}
		a.logger.Info("fetched profile", "profile", profile)
		profileCh <- profile
		return nil
	})

	errgroup.Go(func() error {
		orders, err := a.orderSvc.Fetch(gctx, id)
		if err != nil {
			a.logger.Error("failed to fetch orders", "error", err)
			return err
		}
		a.logger.Info("fetched orders", "orders", orders)
		ordersCh <- orders
		return nil
	})

	if err := errgroup.Wait(); err != nil {
		return "", err
	}

	return "User: " + <-profileCh + " | " + <-ordersCh, nil
}
