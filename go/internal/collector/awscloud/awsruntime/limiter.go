package awsruntime

import (
	"context"
	"sync"
)

// AccountLimiter enforces per-account in-process AWS claim concurrency.
type AccountLimiter struct {
	mu        sync.Mutex
	slots     map[string]chan struct{}
	active    map[string]int64
	maxActive map[string]int
}

// NewAccountLimiter builds a per-account limiter from target scopes. A target
// with no explicit cap defaults to one active claim for that account.
func NewAccountLimiter(targets []TargetScope) *AccountLimiter {
	limits := make(map[string]int)
	for _, target := range targets {
		maxClaims := target.MaxConcurrentClaims
		if maxClaims <= 0 {
			maxClaims = 1
		}
		if current := limits[target.AccountID]; current == 0 || maxClaims > current {
			limits[target.AccountID] = maxClaims
		}
	}
	slots := make(map[string]chan struct{}, len(limits))
	for accountID, maxClaims := range limits {
		slots[accountID] = make(chan struct{}, maxClaims)
	}
	return &AccountLimiter{
		slots:     slots,
		active:    make(map[string]int64, len(limits)),
		maxActive: limits,
	}
}

// Acquire waits for an account claim slot and returns a release function.
func (l *AccountLimiter) Acquire(ctx context.Context, accountID string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	l.mu.Lock()
	slot := l.slots[accountID]
	l.mu.Unlock()
	if slot == nil {
		return func() {}, nil
	}
	select {
	case slot <- struct{}{}:
		l.mu.Lock()
		l.active[accountID]++
		l.mu.Unlock()
		return func() {
			l.mu.Lock()
			if l.active[accountID] > 0 {
				l.active[accountID]--
			}
			l.mu.Unlock()
			<-slot
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// AWSClaimConcurrency reports active AWS claims by account.
func (l *AccountLimiter) AWSClaimConcurrency(context.Context) (map[string]int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	output := make(map[string]int64, len(l.maxActive))
	for accountID := range l.maxActive {
		output[accountID] = l.active[accountID]
	}
	return output, nil
}
