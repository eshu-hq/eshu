// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
)

type bootstrapDialTracker struct {
	mu      sync.Mutex
	sockets map[*trackedBootstrapSocket]struct{}
	dials   map[*trackedBootstrapDial]struct{}
	closed  bool
}

type trackedBootstrapDial struct {
	cancel context.CancelFunc
}

type trackedBootstrapSocket struct {
	net.Conn
	owner *bootstrapDialTracker
	once  sync.Once
}

func (t *bootstrapDialTracker) wrap(base pgconn.DialFunc) pgconn.DialFunc {
	if base == nil {
		dialer := &net.Dialer{}
		base = dialer.DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithCancel(ctx)
		dial := &trackedBootstrapDial{cancel: cancel}
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			cancel()
			return nil, net.ErrClosed
		}
		if t.dials == nil {
			t.dials = make(map[*trackedBootstrapDial]struct{})
		}
		t.dials[dial] = struct{}{}
		t.mu.Unlock()

		conn, err := base(dialCtx, network, address)
		t.mu.Lock()
		delete(t.dials, dial)
		if err != nil {
			t.mu.Unlock()
			cancel()
			return nil, err
		}
		if t.closed || dialCtx.Err() != nil {
			t.mu.Unlock()
			cancel()
			_ = conn.Close()
			return nil, net.ErrClosed
		}
		if t.sockets == nil {
			t.sockets = make(map[*trackedBootstrapSocket]struct{})
		}
		tracked := &trackedBootstrapSocket{Conn: conn, owner: t}
		t.sockets[tracked] = struct{}{}
		t.mu.Unlock()
		cancel()
		return tracked, nil
	}
}

func (c *trackedBootstrapSocket) Close() error {
	var err error
	c.once.Do(func() {
		c.owner.mu.Lock()
		delete(c.owner.sockets, c)
		c.owner.mu.Unlock()
		err = c.Conn.Close()
	})
	return err
}

func (t *bootstrapDialTracker) closeAll() error {
	t.mu.Lock()
	t.closed = true
	sockets := make([]*trackedBootstrapSocket, 0, len(t.sockets))
	for socket := range t.sockets {
		sockets = append(sockets, socket)
	}
	dials := make([]*trackedBootstrapDial, 0, len(t.dials))
	for dial := range t.dials {
		dials = append(dials, dial)
	}
	t.mu.Unlock()

	for _, dial := range dials {
		dial.cancel()
	}
	var closeErr error
	for _, socket := range sockets {
		closeErr = errors.Join(closeErr, socket.Close())
	}
	return closeErr
}

func (t *bootstrapDialTracker) activeSockets() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sockets)
}
