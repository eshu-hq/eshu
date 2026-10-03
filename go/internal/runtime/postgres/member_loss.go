// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
)

// readerMemberLoss is a narrow marker for an established fleet snapshot whose
// physical connection disappeared. Callers may recognize its interface with
// errors.As and retry the entire request, never an individual SQL statement.
type readerMemberLoss struct{}

func (readerMemberLoss) Error() string          { return "PostgreSQL reader member lost" }
func (readerMemberLoss) ReaderMemberLost() bool { return true }

func memberQueryFailure(err error, fleet bool) error {
	if !fleet || err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var networkError *net.OpError
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, driver.ErrBadConn) || errors.As(err, &networkError) {
		return errors.Join(readerMemberLoss{}, err)
	}
	return err
}
