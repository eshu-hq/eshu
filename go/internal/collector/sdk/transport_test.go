// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
)

func TestIsTransientTransportError(t *testing.T) {
	t.Parallel()

	reset := &url.Error{Op: "Get", URL: "https://example.test/x", Err: &net.OpError{
		Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET),
	}}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "connection reset through url.Error", err: reset, want: true},
		{name: "wrapped connection reset", err: fmt.Errorf("get page: %w", reset), want: true},
		{name: "connection refused", err: syscall.ECONNREFUSED, want: true},
		{name: "broken pipe", err: syscall.EPIPE, want: true},
		{name: "unexpected EOF", err: fmt.Errorf("decode: %w", io.ErrUnexpectedEOF), want: true},
		{name: "server closed before response", err: &url.Error{Op: "Get", Err: io.EOF}, want: true},
		{name: "net timeout", err: &net.DNSError{IsTimeout: true}, want: true},
		{name: "temporary dns", err: &net.DNSError{IsTemporary: true}, want: true},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "certificate misconfiguration", err: x509.UnknownAuthorityError{}, want: false},
		{name: "dns not found", err: &net.DNSError{IsNotFound: true}, want: false},
		{name: "context canceled error", err: context.Canceled, want: false},
		{name: "reset joined with context canceled", err: errors.Join(reset, context.Canceled), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTransientTransportError(context.Background(), tt.err); got != tt.want {
				t.Fatalf("IsTransientTransportError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTransientTransportErrorNeverClassifiesParentCancellation(t *testing.T) {
	t.Parallel()

	reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if IsTransientTransportError(ctx, reset) {
		t.Fatal("IsTransientTransportError() = true for a cancelled parent context, want false")
	}
	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 0)
	defer deadlineCancel()
	<-deadlineCtx.Done()
	timeout := &url.Error{Op: "Get", Err: context.DeadlineExceeded}
	if IsTransientTransportError(deadlineCtx, timeout) {
		t.Fatal("IsTransientTransportError() = true after parent deadline, want false")
	}
}

func TestIsTransientTransportErrorTreatsClientTimeoutAsTransient(t *testing.T) {
	t.Parallel()

	// http.Client.Timeout surfaces as a net.Error with Timeout()==true that also
	// matches context.DeadlineExceeded; the parent context is still live.
	err := &url.Error{Op: "Get", URL: "https://example.test", Err: timeoutError{}}
	if !IsTransientTransportError(context.Background(), err) {
		t.Fatal("IsTransientTransportError() = false for a per-request client timeout, want true")
	}
}

type timeoutError struct{}

func (timeoutError) Error() string        { return "context deadline exceeded (Client.Timeout exceeded)" }
func (timeoutError) Timeout() bool        { return true }
func (timeoutError) Temporary() bool      { return true }
func (timeoutError) Is(target error) bool { return target == context.DeadlineExceeded }
