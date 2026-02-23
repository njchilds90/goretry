package goretry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/njchilds90/goretry"
)

var errTransient = errors.New("transient error")

func TestDo_SuccessFirstAttempt(t *testing.T) {
	calls := 0
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return nil
	}, goretry.WithMaxAttempts(3))
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDo_RetriesAndSucceeds(t *testing.T) {
	calls := 0
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errTransient
		}
		return nil
	},
		goretry.WithMaxAttempts(5),
		goretry.WithFixedBackoff(1*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDo_MaxAttemptsReached(t *testing.T) {
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxAttempts(3),
		goretry.WithFixedBackoff(1*time.Millisecond),
	)
	if !errors.Is(err, goretry.ErrMaxAttemptsReached) {
		t.Fatalf("expected ErrMaxAttemptsReached, got %v", err)
	}
}

func TestDo_MaxElapsedTime(t *testing.T) {
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxElapsedTime(20*time.Millisecond),
		goretry.WithFixedBackoff(5*time.Millisecond),
	)
	if !errors.Is(err, goretry.ErrMaxElapsedTimeReached) {
		t.Fatalf("expected ErrMaxElapsedTimeReached, got %v", err)
	}
}

func TestDo_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := goretry.Do(ctx, func(ctx context.Context) error {
		return errTransient
	}, goretry.WithMaxAttempts(5))
	if !errors.Is(err, goretry.ErrContextCanceled) {
		t.Fatalf("expected ErrContextCanceled, got %v", err)
	}
}

func TestDo_RetryIf_StopsOnNonRetryable(t *testing.T) {
	permanent := errors.New("permanent")
	calls := 0
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return permanent
	},
		goretry.WithMaxAttempts(10),
		goretry.WithFixedBackoff(1*time.Millisecond),
		goretry.WithRetryIf(func(err error) bool {
			return !errors.Is(err, permanent)
		}),
	)
	if !errors.Is(err, permanent) {
		t.Fatalf("expected permanent error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDo_OnRetryHook(t *testing.T) {
	retries := 0
	goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxAttempts(4),
		goretry.WithFixedBackoff(1*time.Millisecond),
		goretry.WithOnRetry(func(attempt int, err error) {
			retries++
		}),
	)
	if retries != 3 {
		t.Fatalf("expected 3 retry hooks, got %d", retries)
	}
}

func TestDo_OnSuccessHook(t *testing.T) {
	succeeded := false
	goretry.Do(context.Background(), func(ctx context.Context) error {
		return nil
	},
		goretry.WithMaxAttempts(3),
		goretry.WithOnSuccess(func(attempt int) {
			succeeded = true
		}),
	)
	if !succeeded {
		t.Fatal("expected onSuccess hook to be called")
	}
}

func TestDo_OnFailedHook(t *testing.T) {
	failed := false
	goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxAttempts(2),
		goretry.WithFixedBackoff(1*time.Millisecond),
		goretry.WithOnFailed(func(attempts int, err error) {
			failed = true
		}),
	)
	if !failed {
		t.Fatal("expected onFailed hook to be called")
	}
}

func TestDo_ExponentialBackoff(t *testing.T) {
	calls := 0
	start := time.Now()
	goretry.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return errTransient
	},
		goretry.WithMaxAttempts(3),
		goretry.WithExponentialBackoff(10*time.Millisecond, 2.0),
	)
	elapsed := time.Since(start)
	// At minimum: 10ms + 20ms = 30ms
	if elapsed < 25*time.Millisecond {
		t.Fatalf("expected exponential backoff delay, elapsed: %v", elapsed)
	}
}

func TestDo_LinearBackoff(t *testing.T) {
	start := time.Now()
	goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxAttempts(3),
		goretry.WithLinearBackoff(10*time.Millisecond),
	)
	elapsed := time.Since(start)
	if elapsed < 20*time.Millisecond {
		t.Fatalf("expected linear backoff delay, elapsed: %v", elapsed)
	}
}

func TestDo_MaxDelay(t *testing.T) {
	start := time.Now()
	goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxAttempts(4),
		goretry.WithExponentialBackoff(100*time.Millisecond, 10.0),
		goretry.WithMaxDelay(20*time.Millisecond),
	)
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("max delay not respected, elapsed: %v", elapsed)
	}
}

func TestDo_RecoverPanic(t *testing.T) {
	calls := 0
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		calls++
		if calls < 3 {
			panic("something went wrong")
		}
		return nil
	},
		goretry.WithMaxAttempts(5),
		goretry.WithFixedBackoff(1*time.Millisecond),
		goretry.WithRecoverPanic(true),
	)
	if err != nil {
		t.Fatalf("expected success after panic recovery, got %v", err)
	}
}

func TestDoWithResult_Success(t *testing.T) {
	result, err := goretry.DoWithResult(context.Background(),
		func(ctx context.Context) (int, error) {
			return 42, nil
		},
		goretry.WithMaxAttempts(3),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
}

func TestDoWithResult_RetriesAndReturnsValue(t *testing.T) {
	calls := 0
	result, err := goretry.DoWithResult(context.Background(),
		func(ctx context.Context) (string, error) {
			calls++
			if calls < 3 {
				return "", errTransient
			}
			return "ok", nil
		},
		goretry.WithMaxAttempts(5),
		goretry.WithFixedBackoff(1*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected 'ok', got %q", result)
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := goretry.NewCircuitBreaker(goretry.CircuitBreakerConfig{
		MaxFailures:  3,
		OpenDuration: 1 * time.Hour,
	})
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.State() != goretry.StateOpen {
		t.Fatal("expected circuit to be open")
	}
}

func TestCircuitBreaker_BlocksWhenOpen(t *testing.T) {
	cb := goretry.NewCircuitBreaker(goretry.CircuitBreakerConfig{
		MaxFailures:  1,
		OpenDuration: 1 * time.Hour,
	})
	cb.RecordFailure()

	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		return nil
	},
		goretry.WithMaxAttempts(3),
		goretry.WithCircuitBreaker(cb),
	)
	if !errors.Is(err, goretry.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_ClosesAfterSuccess(t *testing.T) {
	cb := goretry.NewCircuitBreaker(goretry.CircuitBreakerConfig{
		MaxFailures:  3,
		OpenDuration: 1 * time.Hour,
	})
	cb.RecordFailure()
	cb.RecordSuccess()
	if cb.State() != goretry.StateClosed {
		t.Fatal("expected circuit to be closed after success")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := goretry.NewCircuitBreaker(goretry.CircuitBreakerConfig{MaxFailures: 1})
	cb.RecordFailure()
	cb.Reset()
	if cb.State() != goretry.StateClosed {
		t.Fatal("expected closed after reset")
	}
}

func TestDo_JitterFull(t *testing.T) {
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxAttempts(3),
		goretry.WithExponentialBackoff(10*time.Millisecond, 2.0),
		goretry.WithJitter(goretry.FullJitter),
	)
	if !errors.Is(err, goretry.ErrMaxAttemptsReached) {
		t.Fatalf("expected ErrMaxAttemptsReached, got %v", err)
	}
}

func TestDo_JitterEqual(t *testing.T) {
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		return errTransient
	},
		goretry.WithMaxAttempts(3),
		goretry.WithExponentialBackoff(10*time.Millisecond, 2.0),
		goretry.WithJitter(goretry.EqualJitter),
	)
	if !errors.Is(err, goretry.ErrMaxAttemptsReached) {
		t.Fatalf("expected ErrMaxAttemptsReached, got %v", err)
	}
}
