// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"database/sql/driver"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/stdlib"
)

// pgxConn is the set of driver interfaces *stdlib.Conn implements and
// database/sql consults. The wrapper forwards every one, so the pool keeps
// pgx's context-aware statements, session reset, ping, and argument checking.
type pgxConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.NamedValueChecker
	driver.SessionResetter
}

var _ pgxConn = (*stdlib.Conn)(nil)

// Option configures NewConnector.
type Option func(*observer)

// WithLogger sets the logger for the operator record emitted once per bounded
// failure. The default is slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(o *observer) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// Connector wraps a driver.Connector so the connections it makes return bounded
// errors.
type Connector struct {
	inner driver.Connector
	obs   *observer
}

// NewConnector wraps inner. inner must yield connections that implement the
// pgx stdlib driver interfaces; Connect fails closed on anything else.
func NewConnector(inner driver.Connector, opts ...Option) *Connector {
	obs := &observer{logger: slog.Default()}
	for _, opt := range opts {
		opt(obs)
	}
	return &Connector{inner: inner, obs: obs}
}

// Connect dials through the wrapped connector and bounds a dial error.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, c.obs.bound(ctx, opConnect, "", err)
	}
	inner, ok := raw.(pgxConn)
	if !ok {
		_ = raw.Close()
		return nil, fmt.Errorf("boundederr: driver connection %T does not implement the pgx stdlib interfaces", raw)
	}
	return &conn{inner: inner, obs: c.obs}, nil
}

// Driver returns the wrapped connector's driver.
func (c *Connector) Driver() driver.Driver { return c.inner.Driver() }
