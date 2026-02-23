# goretry

[![Go Reference](https://pkg.go.dev/badge/github.com/njchilds90/goretry.svg)](https://pkg.go.dev/github.com/njchilds90/goretry)
[![Go Report Card](https://goreportcard.com/badge/github.com/njchilds90/goretry)](https://goreportcard.com/report/github.com/njchilds90/goretry)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**goretry** is a production-ready, zero-dependency retry library for Go.  
Inspired by Python's `tenacity`, Node's `retry`, and Java's `resilience4j`.

---

## Features

- Exponential backoff with optional full/equal jitter
- Fixed and linear backoff strategies
- Context cancellation & deadline support
- Circuit breaker (Closed → Open → Half-Open)
- Before/After/OnError hooks
- Retry on specific error types or custom predicates
- Max attempts, max elapsed time, max delay cap
- `RetryWithResult[T]` generic typed return
- Panic recovery option
- Thread-safe circuit breaker state
- Zero external dependencies

---

## Install
```bash
go get github.com/njchilds90/goretry
```

---

## Quick Start
```go
package main

import (
    "context"
    "fmt"
    "github.com/njchilds90/goretry"
)

func main() {
    ctx := context.Background()

    err := goretry.Do(ctx,
        func(ctx context.Context) error {
            return callExternalAPI()
        },
        goretry.WithMaxAttempts(5),
        goretry.WithExponentialBackoff(100*time.Millisecond, 2.0),
        goretry.WithJitter(goretry.FullJitter),
        goretry.WithMaxDelay(10*time.Second),
    )
    if err != nil {
        fmt.Println("failed:", err)
    }
}
```

---

## Usage

### Fixed Backoff
```go
goretry.Do(ctx, fn,
    goretry.WithMaxAttempts(3),
    goretry.WithFixedBackoff(500*time.Millisecond),
)
```

### Exponential Backoff + Jitter
```go
goretry.Do(ctx, fn,
    goretry.WithMaxAttempts(10),
    goretry.WithExponentialBackoff(200*time.Millisecond, 2.0),
    goretry.WithJitter(goretry.EqualJitter),
    goretry.WithMaxDelay(30*time.Second),
)
```

### Max Elapsed Time
```go
goretry.Do(ctx, fn,
    goretry.WithMaxElapsedTime(2*time.Minute),
    goretry.WithExponentialBackoff(100*time.Millisecond, 1.5),
)
```

### Retry on Specific Errors
```go
goretry.Do(ctx, fn,
    goretry.WithMaxAttempts(5),
    goretry.WithRetryIf(func(err error) bool {
        return errors.Is(err, ErrTransient)
    }),
)
```

### Generic Typed Result
```go
result, err := goretry.DoWithResult(ctx,
    func(ctx context.Context) (string, error) {
        return fetchData()
    },
    goretry.WithMaxAttempts(3),
    goretry.WithFixedBackoff(100*time.Millisecond),
)
```

### Hooks
```go
goretry.Do(ctx, fn,
    goretry.WithMaxAttempts(5),
    goretry.WithOnRetry(func(attempt int, err error) {
        log.Printf("attempt %d failed: %v", attempt, err)
    }),
    goretry.WithOnSuccess(func(attempt int) {
        log.Printf("succeeded on attempt %d", attempt)
    }),
)
```

### Circuit Breaker
```go
cb := goretry.NewCircuitBreaker(goretry.CircuitBreakerConfig{
    MaxFailures:  5,
    OpenDuration: 30 * time.Second,
    HalfOpenMax:  2,
})

goretry.Do(ctx, fn,
    goretry.WithMaxAttempts(10),
    goretry.WithCircuitBreaker(cb),
)
```

### Panic Recovery
```go
goretry.Do(ctx, fn,
    goretry.WithMaxAttempts(3),
    goretry.WithRecoverPanic(true),
)
```

---

## Configuration Reference

| Option | Description |
|---|---|
| `WithMaxAttempts(n)` | Maximum number of attempts |
| `WithMaxElapsedTime(d)` | Stop retrying after total elapsed time |
| `WithFixedBackoff(d)` | Fixed delay between attempts |
| `WithLinearBackoff(d)` | Linearly increasing delay |
| `WithExponentialBackoff(base, multiplier)` | Exponential delay |
| `WithMaxDelay(d)` | Cap on maximum delay |
| `WithJitter(JitterType)` | `FullJitter`, `EqualJitter`, or `NoJitter` |
| `WithRetryIf(func)` | Custom predicate to decide whether to retry |
| `WithOnRetry(func)` | Hook called before each retry |
| `WithOnSuccess(func)` | Hook called on success |
| `WithOnFailed(func)` | Hook called when all attempts exhausted |
| `WithCircuitBreaker(cb)` | Attach a circuit breaker |
| `WithRecoverPanic(bool)` | Recover panics and convert to errors |

---

## Error Types
```go
var ErrMaxAttemptsReached = errors.New("goretry: max attempts reached")
var ErrMaxElapsedTimeReached = errors.New("goretry: max elapsed time reached")
var ErrCircuitOpen = errors.New("goretry: circuit breaker is open")
var ErrContextCanceled = errors.New("goretry: context canceled")
```

---

## Comparison

| Feature | goretry | avast/retry-go | cenkalti/backoff |
|---|---|---|---|
| Generics (`DoWithResult`) | ✅ | ❌ | ❌ |
| Circuit Breaker | ✅ | ❌ | ❌ |
| Linear Backoff | ✅ | ❌ | ❌ |
| Jitter strategies | ✅ Full/Equal | ✅ | ✅ |
| Panic recovery | ✅ | ❌ | ❌ |
| Zero dependencies | ✅ | ✅ | ✅ |
| Hooks (retry/success/fail) | ✅ | ✅ | ❌ |

---

## Contributing

PRs welcome! Please open an issue first for major changes.

---

## License

[MIT](LICENSE) © 2025 njchilds90
