package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type DBPool struct {
	conns []net.Conn
}

func NewDBPool(size int) (*DBPool, error) {
	pool := &DBPool{conns: make([]net.Conn, 0, size)}
	for range size {
		c, _ := net.Pipe()
		pool.conns = append(pool.conns, c)
	}
	return pool, nil
}

func (p *DBPool) Close() error {
	var errs []error
	for _, c := range p.conns {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("closing connections: %v", errs)
	}
	return nil
}

type CacheWarmer struct {
	interval time.Duration
	logger   *log.Logger
}

func NewCacheWarmer(interval time.Duration, logger *log.Logger) *CacheWarmer {
	return &CacheWarmer{interval: interval, logger: logger}
}

func (w *CacheWarmer) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.logger.Println("cache warmer: tick")
		case <-ctx.Done():
			return
		}
	}
}

type Option func(*Server)

func WithPort(port string) Option {
	return func(s *Server) { s.port = port }
}

func WithWorkerPoolSize(size int) Option {
	return func(s *Server) { s.workerPoolSize = size }
}

func WithRequestTimeout(d time.Duration) Option {
	return func(s *Server) { s.requestTimeout = d }
}

func WithShutdownTimeout(d time.Duration) Option {
	return func(s *Server) { s.shutdownTimeout = d }
}

func WithDBPoolSize(size int) Option {
	return func(s *Server) { s.dbPoolSize = size }
}

func WithCacheInterval(d time.Duration) Option {
	return func(s *Server) { s.cacheInterval = d }
}

func WithLogger(l *log.Logger) Option {
	return func(s *Server) { s.logger = l }
}

type job struct {
	r      *http.Request
	result chan []byte
}

type Server struct {
	port            string
	workerPoolSize  int
	requestTimeout  time.Duration
	shutdownTimeout time.Duration
	dbPoolSize      int
	cacheInterval   time.Duration
	logger          *log.Logger

	httpServer *http.Server
	db         *DBPool
	warmer     *CacheWarmer
	wg         sync.WaitGroup

	warmerCancel context.CancelFunc
	warmerDone   chan struct{}

	jobs          chan job
	quit          chan struct{}
	ready         chan struct{}
	shutdownOrder []string
}

func NewServer(opts ...Option) (*Server, error) {
	s := &Server{
		port:            ":8080",
		workerPoolSize:  4,
		requestTimeout:  5 * time.Second,
		shutdownTimeout: 10 * time.Second,
		dbPoolSize:      5,
		cacheInterval:   30 * time.Second,
		logger:          log.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	db, err := NewDBPool(s.dbPoolSize)
	if err != nil {
		return nil, fmt.Errorf("creating db pool: %w", err)
	}
	s.db = db
	s.warmer = NewCacheWarmer(s.cacheInterval, s.logger)

	s.jobs = make(chan job)
	s.quit = make(chan struct{})
	s.ready = make(chan struct{})
	s.warmerDone = make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)
	mux.HandleFunc("/slow", s.handleSlowRequest)

	s.httpServer = &http.Server{
		Handler: mux,
	}

	return s, nil
}

func (s *Server) Start(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	ln, err := net.Listen("tcp", s.port)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	s.httpServer.Addr = ":" + port

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	for i := range s.workerPoolSize {
		s.wg.Add(1)
		go s.worker(i)
	}

	warmerCtx, warmerCancel := context.WithCancel(context.Background())
	s.warmerCancel = warmerCancel
	go func() {
		s.warmer.Run(warmerCtx)
		close(s.warmerDone)
	}()

	s.logger.Printf("server listening on %s", s.httpServer.Addr)
	close(s.ready)

	<-ctx.Done()
	return nil
}

// Ready returns a channel that is closed once the server is listening.
func (s *Server) Ready() <-chan struct{} {
	return s.ready
}

func (s *Server) worker(id int) {
	defer s.wg.Done()
	for {
		select {
		case j := <-s.jobs:
			s.logger.Printf("worker %d: processing %s", id, j.r.URL.Path)
			j.result <- []byte(fmt.Sprintf("processed by worker %d", id))
		case <-s.quit:
			return
		}
	}
}

func (s *Server) Stop(ctx context.Context) error {
	s.logger.Println("shutting down")
	var shutdownErr error

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.httpServer.Close()
		shutdownErr = fmt.Errorf("http shutdown: %w", err)
	}
	s.shutdownOrder = append(s.shutdownOrder, "http")

	close(s.quit)
	workersDone := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-ctx.Done():
		if shutdownErr == nil {
			shutdownErr = fmt.Errorf("shutdown timeout: %w", ctx.Err())
		}
	}
	s.shutdownOrder = append(s.shutdownOrder, "workers")

	s.warmerCancel()
	select {
	case <-s.warmerDone:
	case <-ctx.Done():
		if shutdownErr == nil {
			shutdownErr = fmt.Errorf("shutdown timeout waiting for warmer: %w", ctx.Err())
		}
	}
	s.shutdownOrder = append(s.shutdownOrder, "warmer")

	if err := s.db.Close(); err != nil && shutdownErr == nil {
		shutdownErr = fmt.Errorf("db close: %w", err)
	}
	s.shutdownOrder = append(s.shutdownOrder, "db")

	return shutdownErr
}

func (s *Server) ShutdownOrder() []string {
	return s.shutdownOrder
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()

	j := job{
		r:      r,
		result: make(chan []byte, 1),
	}

	select {
	case s.jobs <- j:
		select {
		case data := <-j.result:
			w.WriteHeader(http.StatusOK)
			w.Write(data)
		case <-ctx.Done():
			http.Error(w, "request timeout", http.StatusGatewayTimeout)
		}
	case <-ctx.Done():
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}
}

func (s *Server) handleSlowRequest(w http.ResponseWriter, r *http.Request) {
	select {
	case <-time.After(20 * time.Second):
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "slow response")
	case <-r.Context().Done():
	}
}
