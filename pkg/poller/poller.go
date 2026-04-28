package poller

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Poller drives a retry loop until fn signals completion or the context expires.
type Poller interface {
	Poll(ctx context.Context, fn func(ctx context.Context) (bool, error)) error
}

// Strategy controls how inter-poll sleep time is calculated.
type Strategy int

const (
	StrategyBackoff Strategy = iota // exponential: min * 2^attempt, capped at max
	StrategyFixed                   // constant: min
	StrategyLinear                  // linear ramp from min to max
)

// StrategyFromString parses a strategy name, defaulting to backoff.
func StrategyFromString(s string) Strategy {
	switch s {
	case "fixed":
		return StrategyFixed
	case "linear":
		return StrategyLinear
	default:
		return StrategyBackoff
	}
}

// Options configures a Poller.
type Options struct {
	Min      time.Duration
	Max      time.Duration
	Strategy Strategy
	Jitter   bool // add up to 25% random jitter to sleep duration
}

type poller struct {
	opts Options
}

// New returns a Poller with the given options. Min and Max default to 5s / 2m
// if left as zero.
func New(opts Options) Poller {
	if opts.Min == 0 {
		opts.Min = 5 * time.Second
	}
	if opts.Max == 0 {
		opts.Max = 2 * time.Minute
	}
	if opts.Max < opts.Min {
		opts.Max = opts.Min
	}
	return &poller{opts: opts}
}

// Poll calls fn immediately and, if fn returns (false, nil), sleeps and retries
// until fn returns (true, nil), fn returns a non-nil error, or ctx is done.
func (p *poller) Poll(ctx context.Context, fn func(ctx context.Context) (bool, error)) error {
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return fmt.Errorf("context cancelled before poll attempt %d: %w", attempt, ctx.Err())
		}

		done, err := fn(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}

		sleep := p.sleepDuration(attempt)
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting (attempt %d): %w", attempt, ctx.Err())
		case <-time.After(sleep):
		}
	}
}

func (p *poller) sleepDuration(attempt int) time.Duration {
	var d time.Duration
	switch p.opts.Strategy {
	case StrategyFixed:
		d = p.opts.Min
	case StrategyLinear:
		// linear ramp: min + attempt * step, capped at max
		step := time.Duration(0)
		if p.opts.Max > p.opts.Min {
			// estimate a cap of ~10 attempts for the ramp
			step = (p.opts.Max - p.opts.Min) / 10
		}
		d = p.opts.Min + time.Duration(attempt)*step
		if d > p.opts.Max {
			d = p.opts.Max
		}
	default: // StrategyBackoff
		d = time.Duration(float64(p.opts.Min) * math.Pow(2, float64(attempt)))
		if d > p.opts.Max {
			d = p.opts.Max
		}
	}

	if p.opts.Jitter && d > 0 {
		// add up to 25% jitter
		jitter := rand.Int63n(int64(d / 4))
		d += time.Duration(jitter)
	}
	return d
}
