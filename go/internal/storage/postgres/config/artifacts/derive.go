// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"log/slog"
	"time"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// DefaultDeriveTimeout is the deadline of one derive when DeriverConfig sets
// none (protocol section 3, failure semantics).
const DefaultDeriveTimeout = 10 * time.Second

// Database is the connection pool a Deriver reads and writes through.
type Database interface {
	db.ExecQueryer
	db.Beginner
}

// DeriverConfig configures a Deriver.
type DeriverConfig struct {
	ExtractorVersion int
	Encoding         Encoding
	Fence            Fence
	Timeout          time.Duration
	Logger           *slog.Logger
}

// Request names the repository one derive runs for.
type Request struct {
	RepoID       string
	ScopeID      string
	GenerationID string
	Trigger      Trigger
}

// Result is what one derive did.
type Result struct {
	RepoID          string
	Trigger         Trigger
	Outcome         Outcome
	ContentVersion  int64
	Rows            int
	Stats           contentartifacts.ReadStats
	Duration        time.Duration
	ErrorClass      string
	Err             error
	FailureRecorded bool
}

type loadFunc func(
	ctx context.Context,
	reader contentartifacts.Reader,
	repo contentartifacts.Repository,
	files []contentartifacts.File,
	hasFileList bool,
	allowance int,
) ([]map[string]any, contentartifacts.ReadStats, error)

// Deriver derives the stored config-artifact rows of one repository.
type Deriver struct {
	database         Database
	extractorVersion int
	timeout          time.Duration
	load             loadFunc
	afterVersionRead func(context.Context)
	afterLock        func(context.Context)
}

type contentReader struct{ queryer db.Queryer }

func (r contentReader) ListFiles(context.Context, string, int) ([]contentartifacts.File, error) {
	return nil, nil
}

func (r contentReader) ReadFiles(context.Context, string, []string) ([]contentartifacts.File, error) {
	return nil, nil
}

func (r contentReader) NonBlankPaths(context.Context, string, []string) ([]string, error) {
	return nil, nil
}

// NewDeriver returns a Deriver over database. Stub.
func NewDeriver(database Database, config DeriverConfig) (*Deriver, error) {
	return &Deriver{database: database, extractorVersion: config.ExtractorVersion}, errors.New("not implemented")
}

// DeriveRepository runs one derive. Stub.
func (d *Deriver) DeriveRepository(ctx context.Context, req Request) Result {
	return Result{RepoID: req.RepoID, Trigger: req.Trigger}
}

type writeState struct {
	contentVersion   *int64
	extractorVersion int
	rowCount         *int
}

func mayWrite(state *writeState, v0 int64, extractorVersion int) bool { return false }

func storedRows(built []map[string]any) ([]Row, error) { return nil, nil }
