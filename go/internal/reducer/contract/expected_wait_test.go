// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"errors"
	"fmt"
	"testing"
)

type expectedWaitStub struct{ retryable, wait bool }

func (expectedWaitStub) Error() string        { return "waiting" }
func (e expectedWaitStub) Retryable() bool    { return e.retryable }
func (e expectedWaitStub) ExpectedWait() bool { return e.wait }

func TestIsExpectedWait(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"retryable expected wait", expectedWaitStub{retryable: true, wait: true}, true},
		{"wrapped expected wait", fmt.Errorf("handle: %w", expectedWaitStub{retryable: true, wait: true}), true},
		{"retryable but not a wait", expectedWaitStub{retryable: true}, false},
		{"wait that is not retryable", expectedWaitStub{wait: true}, false},
	}
	for _, tt := range tests {
		if got := IsExpectedWait(tt.err); got != tt.want {
			t.Errorf("%s: IsExpectedWait() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
