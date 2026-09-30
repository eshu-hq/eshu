// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"context"
	"errors"
)

type readContractFixture struct{}

func (readContractFixture) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("unused")
}

func (readContractFixture) QueryRowContext(context.Context, string, ...any) Row {
	return nil
}

func (readContractFixture) BeginReadOnlySnapshot(context.Context) (ReadTransaction, error) {
	return nil, errors.New("unused")
}

var _ ReadStore = readContractFixture{}
