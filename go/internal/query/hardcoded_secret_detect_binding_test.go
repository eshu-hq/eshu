// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// detectArms converts the collector-shared detector's arms to the shape the
// migration parser returns, so the two compare directly.
func detectArms() []secretLineCaseArm {
	sources := detect.ArmSources()
	arms := make([]secretLineCaseArm, 0, len(sources))
	for _, source := range sources {
		arms = append(arms, secretLineCaseArm{regex: source.Regex, kind: source.Kind})
	}
	return arms
}

// TestHardcodedSecretDetectPackageIsBoundToMigration pins #8812's binding: the
// collector's marker redactor and the query layer share go/internal/secret/detect,
// and that package says what migration 138 says. A Go-only edit of the pattern
// or a CASE arm in either place turns this red.
func TestHardcodedSecretDetectPackageIsBoundToMigration(t *testing.T) {
	t.Parallel()

	if hardcodedSecretSQLPattern != detect.SQLPattern {
		t.Fatalf("hardcodedSecretSQLPattern diverged from detect.SQLPattern:\n%s\n%s", hardcodedSecretSQLPattern, detect.SQLPattern)
	}
	err := verifyHardcodedSecretMigrationBinding(
		secretLinesMigrationSQL(t),
		detect.SQLPattern,
		hardcodedSecretSQLSuppressionPredicate(),
		detectArms(),
	)
	if err != nil {
		t.Fatalf("detect package is not bound to migration 138: %v", err)
	}
}

// TestHardcodedSecretDetectBindingFailsOnDetectOnlyEdits is the seeded
// violation: an edit made only in the detect package must be rejected.
func TestHardcodedSecretDetectBindingFailsOnDetectOnlyEdits(t *testing.T) {
	t.Parallel()

	migrationSQL := secretLinesMigrationSQL(t)
	predicate := hardcodedSecretSQLSuppressionPredicate()
	arms := detectArms()
	if err := verifyHardcodedSecretMigrationBinding(migrationSQL, detect.SQLPattern, predicate, arms); err != nil {
		t.Fatalf("clean inputs must pass: %v", err)
	}
	reordered := slices.Clone(arms)
	reordered[0], reordered[1] = reordered[1], reordered[0]
	if err := verifyHardcodedSecretMigrationBinding(migrationSQL, detect.SQLPattern, predicate, reordered); err == nil {
		t.Fatal("binding accepted a reordered CASE arm list")
	}
	widened := slices.Clone(arms)
	widened[len(widened)-1].regex += "|bearer"
	if err := verifyHardcodedSecretMigrationBinding(migrationSQL, detect.SQLPattern, predicate, widened); err == nil {
		t.Fatal("binding accepted a widened CASE arm")
	}
	if err := verifyHardcodedSecretMigrationBinding(migrationSQL, detect.SQLPattern+"|glpat-[A-Za-z0-9]{20}", predicate, arms); err == nil {
		t.Fatal("binding accepted a pattern branch that only the Go side has")
	}
}
