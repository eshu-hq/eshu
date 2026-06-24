// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// secretsIAMGraphProjectionEnabledEnv opts the reducer into live secrets/IAM
// graph projection (ADR #1314 §4). It defaults OFF: the writer stays nil and
// DomainSecretsIAMGraphProjection stays unregistered until a target-bound
// activation decision and flag-on proof are recorded. The repo-local §11/§12
// proofs and §14 principal+security sign-off exist; turning this on without the
// target deployment record is a rule violation, not a config choice.
const secretsIAMGraphProjectionEnabledEnv = "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED"

// secretsIAMGraphProjectionEnabled reports whether the operator explicitly
// opted into live secrets/IAM graph projection. An empty value is OFF; a
// malformed value is an error rather than a silent default so a typo never
// reads as either state.
func secretsIAMGraphProjectionEnabled(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv(secretsIAMGraphProjectionEnabledEnv))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", secretsIAMGraphProjectionEnabledEnv, raw, err)
	}
	return enabled, nil
}

// secretsIAMGraphProjectionWriter returns the live graph writer when the
// opt-in flag is set, or nil (keeping DomainSecretsIAMGraphProjection
// unregistered) when it is not. Returning the interface type rather than the
// concrete pointer keeps a disabled run's handler field a true nil so the
// additive registry gate sees no writer.
func secretsIAMGraphProjectionWriter(
	getenv func(string) string,
	executor sourcecypher.Executor,
	batchSize int,
	logger *slog.Logger,
) (reducer.SecretsIAMGraphWriter, error) {
	enabled, err := secretsIAMGraphProjectionEnabled(getenv)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	if logger != nil {
		logger.Warn("secrets/IAM graph projection ENABLED: live exact-only graph writes are active",
			"env_var", secretsIAMGraphProjectionEnabledEnv,
			"domain", string(reducer.DomainSecretsIAMGraphProjection))
	}
	return sourcecypher.NewSecretsIAMGraphWriter(executor, batchSize), nil
}
