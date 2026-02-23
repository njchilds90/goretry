// Package goretry provides a production-ready retry library for Go with
// exponential backoff, jitter, context support, circuit breaker, and hooks.
//
// Basic usage:
//
//	err := goretry.Do(ctx, fn,
//	    goretry.WithMaxAttempts(5),
//	    goretry.WithExponentialBackoff(100*time.Millisecond, 2.0),
//	    goretry.WithJitter(goretry.FullJitter),
//	)
package goretry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Sentinel errors returned by goretry.
var (
	ErrMaxAttemptsReached  = errors.New("goretry: max attempts reached")
	ErrMaxElapsedTimeReached = errors.New("goretry: max elapsed time reached")
	ErrCircuitOpen         = errors.New("goretry: circuit breaker is open")
	ErrContextCanceled     = errors.New("goretry: context canceled")
)

// JitterType controls how jitter is applied to backoff delays.
type JitterType int

const (
	// NoJitter applies no jitter — delay is deterministic.
	NoJitter JitterType = iota
	// FullJitter randomizes the delay in [0, delay).
	FullJitter
	// EqualJitter uses delay/2 + rand(0, delay/2).
	EqualJitter
)

// BackoffStrategy determines how the base delay is calculated per attempt.
type BackoffStrategy int

const (
	// FixedBackoff uses the same delay every attempt.
	FixedBackoff BackoffStrategy = iota
	// LinearBackoff increases delay linearly: base * attempt.
	LinearBackoff
	// ExponentialBackoff increases delay as: base * multiplier^attempt.
	ExponentialBackoff
)

// RetryFunc is the function signature that Do and DoWithResult accept.
type RetryFunc func(ctx context.Context) error

// OnRetryHook is called before each retry with the attempt number and last error.
type OnRetryHook func(attempt int, err error)

// OnSuccessHook is called when the function succeeds, with the attempt number.
type OnSuccessHook func(attempt int)

// OnFailedHook is called when all retry attempts are exhausted.
type OnFailedHook func(attempts int, lastErr error)

// RetryIfFunc decides whether a given error should trigger a retry.
type RetryIfFunc func(err error) bool

// config holds all resolved retry configuration.
type config struct {
	maxAttempts     int
	maxElapsedTime  time.Duration
	baseDelay       time.Duration
	maxDelay        time.Duration
	multiplier      float64
	strategy        BackoffStrategy
	jitter          JitterType
	retryIf         RetryIfFunc
	onRetry         OnRetryHook
	onSuccess       OnSuccessHook
	onFailed        OnFailedHook
	circuitBreaker  *CircuitBreaker
	recoverPanic    bool
}

// Option is a functional option for configuring retry behavior.
type Option func(*config)

// WithMaxAttempts sets the maximum number of attempts (including the first call).
func WithMaxAttempts(n int) Option {
	return func(c *config) { c.maxAttempts = n }
}

// WithMaxElapsedTime sets the maximum total time allowed across all attempts.
func WithMaxElapsedTime(d time.Duration) Option {
	return func(c *config) { c.maxElapsedTime = d }
}

// WithFixedBackoff sets a fixed delay between attempts.
func WithFixedBackoff(d time.Duration) Option {
	return func(c *config) {
		c.strategy = FixedBackoff
		c.baseDelay = d
	}
}

// WithLinearBackoff sets a linearly increasing delay (base * attemptNumber).
func WithLinearBackoff(base time.Duration) Option {
	return func(c *config) {
		c.strategy = LinearBackoff
		c.baseDelay = base
	}
}

// WithExponentialBackoff sets an exponentially increasing delay.
// base is the starting delay; multiplier is the growth factor (e.g. 2.0).
func WithExponentialBackoff(base time.Duration, multiplier float64) Option {
	return func(c *config) {
		c.strategy = ExponentialBackoff
		c.baseDelay = base
		c.multiplier = multiplier
	}
}

// WithMaxDelay caps the computed delay to a maximum value.
func WithMaxDelay(d time.Duration) Option {
	return func(c *config) { c.maxDelay = d }
}

// WithJitter sets the jitter strategy: NoJitter, FullJitter, or EqualJitter.
func WithJitter(j JitterType) Option {
	return func(c *config) { c.jitter = j }
}

// WithRetryIf sets a custom predicate to decide whether an error should be retried.
// By default all non-nil errors are retried.
func WithRetryIf(fn RetryIfFunc) Option {
	return func(c *config) { c.retryIf = fn }
}

// WithOnRetry registers a hook called before each retry attempt.
func WithOnRetry(fn OnRetryHook) Option {
	return func(c *config) { c.onRetry = fn }
}

// WithOnSuccess registers a hook called when the operation succeeds.
func WithOnSuccess(fn OnSuccessHook) Option {
	return func(c *config) { c.onSuccess = fn }
}

// WithOnFailed registers a hook called when all attempts are exhausted.
func WithOnFailed(fn OnFailedHook) Option {
	return func(c *config) { c.onFailed = fn }
}

// WithCircuitBreaker attaches a CircuitBreaker to the retry loop.
// If the circuit is open, Do returns ErrCircuitOpen immediately.
func WithCircuitBreaker(cb *CircuitBreaker) Option {
	return func(c *config) { c.circuitBreaker = cb }
}

// WithRecoverPanic converts panics in the retried function into errors,
// allowing retry logic to apply even when the function panics.
func WithRecoverPanic(recover bool) Option {
	return func(c *config) { c.recoverPanic = recover }
}

func defaultConfig() *config {
	return &config{
		maxAttempts: 3,
		baseDelay:   100 * time.Millisecond,
		maxDelay:    0, // no cap
		multiplier:  2.0,
		strategy:    ExponentialBackoff,
		jitter:      NoJitter,
		retryIf:     func(err error) bool { return err != nil },
	}
}

// Do executes fn with retry logic governed by the provided options.
// It returns nil on success, or the last error (wrapped with a sentinel) on failure.
func Do(ctx context.Context, fn RetryFunc, opts ...Option) error {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}
	return run(ctx, cfg, fn)
}

// DoWithResult executes a function that returns a value and an error.
// It retries according to the provided options and returns the result on success.
//
// Example:
//
//	result, err := goretry.DoWithResult(ctx,
//	    func(ctx context.Context) (string, error) { return fetch() },
//	    goretry.WithMaxAttempts(3),
//	)
func DoWithResult[T any](ctx context.Context, fn func(context.Context) (T, error), opts ...Option) (T, error) {
	var result T
	err := Do(ctx, func(ctx context.Context) error {
		var e error
		result, e = fn(ctx)
		return e
	}, opts...)
	return result, err
}

func run(ctx context.Context, cfg *config, fn RetryFunc) error {
	start := time.Now()
	var lastErr error

	for attempt := 1; ; attempt++ {
		// Check context before each attempt.
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", ErrContextCanceled, ctx.Err())
		}

		// Check circuit breaker.
		if cfg.circuitBreaker != nil {
			if !cfg.circuitBreaker.Allow() {
				return ErrCircuitOpen
			}
		}

		// Execute with optional panic recovery.
		lastErr = execute(ctx, fn, cfg.recoverPanic)

		// Record circuit breaker result.
		if cfg.circuitBreaker != nil {
			if lastErr == nil {
				cfg.circuitBreaker.RecordSuccess()
			} else {
				cfg.circuitBreaker.RecordFailure()
			}
		}

		if lastErr == nil {
			if cfg.onSuccess != nil {
				cfg.onSuccess(attempt)
			}
			return nil
		}

		// Check if this error should trigger a retry.
		if cfg.retryIf != nil && !cfg.retryIf(lastErr) {
			return lastErr
		}

		// Check stop conditions.
		if cfg.maxAttempts > 0 && attempt >= cfg.maxAttempts {
			if cfg.onFailed != nil {
				cfg.onFailed(attempt, lastErr)
			}
			return fmt.Errorf("%w after %d attempts: %w", ErrMaxAttemptsReached, attempt, lastErr)
		}
		if cfg.maxElapsedTime > 0 && time.Since(start) >= cfg.maxElapsedTime {
			if cfg.onFailed != nil {
				cfg.onFailed(attempt, lastErr)
			}
			return fmt.Errorf("%w: %w", ErrMaxElapsedTimeReached, lastErr)
		}

		delay := computeDelay(cfg, attempt)

		if cfg.onRetry != nil {
			cfg.onRetry(attempt, lastErr)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrContextCanceled, ctx.Err())
		case <-time.After(delay):
		}
	}
}

func execute(ctx context.Context, fn RetryFunc, recoverPanic bool) (err error) {
	if recoverPanic {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("goretry: recovered panic: %v", r)
			}
		}()
	}
	return fn(ctx)
}

func computeDelay(cfg *config, attempt int) time.Duration {
	var base float64

	switch cfg.strategy {
	case FixedBackoff:
		base = float64(cfg.baseDelay)
	case LinearBackoff:
		base = float64(cfg.baseDelay) * float64(attempt)
	case ExponentialBackoff:
		multiplier := cfg.multiplier
		if multiplier <= 0 {
			multiplier = 2.0
		}
		base = float64(cfg.baseDelay) * math.Pow(multiplier, float64(attempt-1))
	}

	if cfg.maxDelay > 0 && base > float64(cfg.maxDelay) {
		base = float64(cfg.maxDelay)
	}

	var delay float64
	switch cfg.jitter {
	case FullJitter:
		delay = rand.Float64() * base
	case EqualJitter:
		delay = base/2 + rand.Float64()*(base/2)
	default:
		delay = base
	}

	return time.Duration(delay)
}

// CircuitBreakerState represents the state of a circuit breaker.
type CircuitBreakerState int

const (
	// StateClosed means the circuit is healthy and requests pass through.
	StateClosed CircuitBreakerState = iota
	// StateOpen means the circuit has tripped and requests are blocked.
	StateOpen
	// StateHalfOpen means the circuit is testing with limited requests.
	StateHalfOpen
)

// CircuitBreakerConfig configures the circuit breaker behavior.
type CircuitBreakerConfig struct {
	// MaxFailures is the number of consecutive failures before opening the circuit.
	MaxFailures int
	// OpenDuration is how long the circuit stays open before moving to half-open.
	OpenDuration time.Duration
	// HalfOpenMax is the number of test requests allowed in half-open state.
	HalfOpenMax int
}

// CircuitBreaker implements the circuit breaker pattern.
// Use NewCircuitBreaker to create one.
//
// States:
//   - Closed: normal operation, failures are counted.
//   - Open: all calls blocked until OpenDuration elapses.
//   - HalfOpen: limited calls allowed to test recovery.
type CircuitBreaker struct {
	mu           sync.Mutex
	cfg          CircuitBreakerConfig
	state        CircuitBreakerState
	failures     int
	halfOpenPass int
	openedAt     time.Time
}

// NewCircuitBreaker creates a CircuitBreaker with the given configuration.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 30 * time.Second
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = 1
	}
	return &CircuitBreaker{cfg: cfg}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.tick()
	return cb.state
}

// Allow returns true if a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.tick()
	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		return false
	case StateHalfOpen:
		if cb.halfOpenPass < cb.cfg.HalfOpenMax {
			cb.halfOpenPass++
			return true
		}
		return false
	}
	return false
}

// RecordSuccess records a successful call, potentially closing the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.halfOpenPass = 0
	cb.state = StateClosed
}

// RecordFailure records a failed call, potentially opening the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == StateHalfOpen || cb.failures >= cb.cfg.MaxFailures {
		cb.state = StateOpen
		cb.openedAt = time.Now()
		cb.halfOpenPass = 0
	}
}

// Reset resets the circuit breaker to its initial closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.failures = 0
	cb.halfOpenPass = 0
}

func (cb *CircuitBreaker) tick() {
	if cb.state == StateOpen && time.Since(cb.openedAt) >= cb.cfg.OpenDuration {
		cb.state = StateHalfOpen
		cb.halfOpenPass = 0
	}
}
