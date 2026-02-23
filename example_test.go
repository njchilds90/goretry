package goretry_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/njchilds90/goretry"
)

func ExampleDo_fixed() {
	calls := 0
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("not ready")
		}
		return nil
	},
		goretry.WithMaxAttempts(5),
		goretry.WithFixedBackoff(1*time.Millisecond),
	)
	fmt.Println(err)
	// Output: <nil>
}

func ExampleDo_exponential() {
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		return nil
	},
		goretry.WithMaxAttempts(3),
		goretry.WithExponentialBackoff(50*time.Millisecond, 2.0),
		goretry.WithJitter(goretry.FullJitter),
	)
	fmt.Println(err)
	// Output: <nil>
}

func ExampleDoWithResult() {
	result, err := goretry.DoWithResult(context.Background(),
		func(ctx context.Context) (string, error) {
			return "hello", nil
		},
		goretry.WithMaxAttempts(3),
	)
	fmt.Println(result, err)
	// Output: hello <nil>
}

func ExampleNewCircuitBreaker() {
	cb := goretry.NewCircuitBreaker(goretry.CircuitBreakerConfig{
		MaxFailures:  5,
		OpenDuration: 30 * time.Second,
		HalfOpenMax:  2,
	})

	calls := 0
	err := goretry.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return nil
	},
		goretry.WithMaxAttempts(3),
		goretry.WithCircuitBreaker(cb),
	)
	fmt.Println(err)
	// Output: <nil>
}
