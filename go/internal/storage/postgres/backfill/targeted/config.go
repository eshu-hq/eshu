// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"errors"
	"fmt"
	"strings"
)

// validatePassConfig rejects a partial store before the pass starts its span
// or reads the database. The caller checks DB, Beginner, and owed partitions
// first to retain the existing error precedence and wording.
func (s Store) validatePassConfig() error {
	if s.PhaseCommitted == nil {
		return errors.New("partition-scoped maintenance phase callback is required")
	}
	return s.validateReopenCollaborators()
}

// DefaultInboundBudgetBytes is the inbound byte budget the partition-scoped
// pass applies to its anchor-scoped inbound fact load when the store does not
// override it (#9781). A normal owed repository's inbound load is a few
// thousand rows after the anchor and marker fixes (about 830 rows measured),
// well under this figure, while a regressed filter that selects the whole
// corpus (about 15 GB) crosses it immediately and fails the pass loudly
// instead of OOM-killing the reducer. Zero (the zero Store) keeps the load
// unbounded for tests and the per-commit path, which never sets a budget.
const DefaultInboundBudgetBytes = 256 << 20

// validateReopenConfig checks the standalone reopen entrypoint. It needs no
// DB or phase callback: it uses only the supplied transaction beginner and
// reopen collaborators.
func (s Store) validateReopenConfig() error {
	if s.Backfill.Beginner == nil {
		return errors.New("transaction beginner is required for partition-scoped maintenance")
	}
	return s.validateReopenCollaborators()
}

// validateReopenCollaborators checks the reopen fields shared by both paths.
func (s Store) validateReopenCollaborators() error {
	if s.GateReopen == nil {
		return errors.New("partition-scoped maintenance reopen gate callback is required")
	}
	if s.ReopenSucceeded == nil {
		return errors.New("partition-scoped maintenance reopen callback is required")
	}
	for _, query := range []struct {
		name string
		text string
	}{
		{"deployment mapping", s.Queries.DeploymentMapping},
		{"code import repo edge", s.Queries.CodeImportRepoEdge},
		{"correlation", s.Queries.Correlation},
		{"deployable unit", s.Queries.DeployableUnit},
	} {
		if strings.TrimSpace(query.text) == "" {
			return fmt.Errorf("partition-scoped maintenance %s query is required", query.name)
		}
	}
	if len(s.CorrelationDomains) == 0 {
		return errors.New("partition-scoped maintenance correlation domains are required")
	}
	seen := make(map[string]struct{}, len(s.CorrelationDomains))
	for i, domain := range s.CorrelationDomains {
		name := strings.TrimSpace(domain)
		if name == "" {
			return fmt.Errorf("partition-scoped maintenance correlation domain %d is blank", i)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("partition-scoped maintenance correlation domain %q is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}
