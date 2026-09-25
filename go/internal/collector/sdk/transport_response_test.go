// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
)

func TestIsTransientTransportErrorRejectsBareEOFFromDecode(t *testing.T) {
	t.Parallel()

	reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "empty success body decodes to bare EOF", err: io.EOF, want: false},
		{name: "wrapped bare EOF", err: fmt.Errorf("decode: %w", io.EOF), want: false},
		{name: "truncated body is unexpected EOF", err: fmt.Errorf("decode: %w", io.ErrUnexpectedEOF), want: true},
		{name: "reset mid body", err: fmt.Errorf("decode: %w", reset), want: true},
		{name: "server closed before response", err: fmt.Errorf("get: %w", &url.Error{Op: "Get", Err: io.EOF}), want: true},
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

func TestTransportFailureClass(t *testing.T) {
	t.Parallel()

	wrap := func(errno syscall.Errno) error {
		return &url.Error{Op: "Get", URL: "https://secret.internal.example/x", Err: &net.OpError{
			Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", errno),
		}}
	}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "refused", err: wrap(syscall.ECONNREFUSED), want: "refused"},
		{name: "reset", err: wrap(syscall.ECONNRESET), want: "reset"},
		{name: "aborted", err: wrap(syscall.ECONNABORTED), want: "reset"},
		{name: "broken pipe", err: wrap(syscall.EPIPE), want: "reset"},
		{name: "host unreachable", err: wrap(syscall.EHOSTUNREACH), want: "unreachable"},
		{name: "net unreachable", err: wrap(syscall.ENETUNREACH), want: "unreachable"},
		{name: "etimedout", err: wrap(syscall.ETIMEDOUT), want: "timeout"},
		{name: "client timeout", err: &url.Error{Op: "Get", Err: timeoutError{}}, want: "timeout"},
		{name: "dns temporary", err: &net.DNSError{IsTemporary: true}, want: "dns"},
		{name: "unexpected eof", err: fmt.Errorf("x: %w", io.ErrUnexpectedEOF), want: "eof"},
		{name: "server closed before response", err: &url.Error{Op: "Get", Err: io.EOF}, want: "eof"},
		{name: "unknown", err: fmt.Errorf("boom"), want: "other"},
		{name: "nil", err: nil, want: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := TransportFailureClass(tt.err); got != tt.want {
				t.Fatalf("TransportFailureClass() = %q, want %q", got, tt.want)
			}
		})
	}
}
