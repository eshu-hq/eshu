package awsruntime

import (
	"context"
	"testing"
)

func TestAccountLimiterReportsAndReleasesActiveClaim(t *testing.T) {
	limiter := NewAccountLimiter([]TargetScope{{
		AccountID:           "123456789012",
		MaxConcurrentClaims: 1,
	}})

	release, err := limiter.Acquire(context.Background(), "123456789012")
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	counts, err := limiter.AWSClaimConcurrency(context.Background())
	if err != nil {
		t.Fatalf("AWSClaimConcurrency() error = %v", err)
	}
	if got := counts["123456789012"]; got != 1 {
		t.Fatalf("active count = %d, want 1", got)
	}

	release()
	counts, err = limiter.AWSClaimConcurrency(context.Background())
	if err != nil {
		t.Fatalf("AWSClaimConcurrency() after release error = %v", err)
	}
	if got := counts["123456789012"]; got != 0 {
		t.Fatalf("active count after release = %d, want 0", got)
	}
}

func TestAccountLimiterHonorsContextWhenAccountAtCapacity(t *testing.T) {
	limiter := NewAccountLimiter([]TargetScope{{
		AccountID:           "123456789012",
		MaxConcurrentClaims: 1,
	}})
	release, err := limiter.Acquire(context.Background(), "123456789012")
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.Acquire(ctx, "123456789012"); err == nil {
		t.Fatalf("Acquire() error = nil, want context cancellation")
	}
}
