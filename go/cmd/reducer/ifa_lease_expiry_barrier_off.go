// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifafaultinjection

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ifaLeaseExpiryBarrier has no behavior outside the fault-injection build.
type ifaLeaseExpiryBarrier struct{}

func newIfaLeaseExpiryBarrier(_ func(string) string) (*ifaLeaseExpiryBarrier, error) {
	return nil, nil
}

func (*ifaLeaseExpiryBarrier) wrapGraph(inner sourcecypher.Executor) sourcecypher.Executor {
	return inner
}

func (*ifaLeaseExpiryBarrier) wrapReducer(inner reducer.Executor) reducer.Executor {
	return inner
}
