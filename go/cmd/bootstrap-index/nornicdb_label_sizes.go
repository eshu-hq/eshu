// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strconv"
	"strings"
)

func nornicDBEntityLabelBatchSizes(getenv func(string) string, entityBatchSize int) (map[string]int, error) {
	return nornicDBLabelSizeMap(
		getenv,
		nornicDBEntityLabelBatchSizesEnv,
		defaultNornicDBEntityLabelBatchSizes(entityBatchSize),
		entityBatchSize,
	)
}

func nornicDBEntityLabelPhaseGroupStatements(getenv func(string) string, entityPhaseStatements int) (map[string]int, error) {
	return nornicDBLabelSizeMap(
		getenv,
		nornicDBEntityLabelPhaseGroupStatementsEnv,
		defaultNornicDBEntityLabelPhaseGroupStatements(entityPhaseStatements),
		entityPhaseStatements,
	)
}

func defaultNornicDBEntityLabelBatchSizes(entityBatchSize int) map[string]int {
	return map[string]int{
		"Function":    capOptionalBatchSize(entityBatchSize, defaultNornicDBFunctionEntityBatchSize),
		"K8sResource": capOptionalBatchSize(entityBatchSize, defaultNornicDBK8sResourceEntityBatchSize),
		"Struct":      capOptionalBatchSize(entityBatchSize, defaultNornicDBStructEntityBatchSize),
		"Variable":    capOptionalBatchSize(entityBatchSize, defaultNornicDBVariableEntityBatchSize),
	}
}

func defaultNornicDBEntityLabelPhaseGroupStatements(entityPhaseStatements int) map[string]int {
	return map[string]int{
		"Function":    capOptionalBatchSize(entityPhaseStatements, defaultNornicDBFunctionEntityPhaseStatements),
		"K8sResource": capOptionalBatchSize(entityPhaseStatements, defaultNornicDBK8sResourceEntityPhaseStatements),
		"Struct":      capOptionalBatchSize(entityPhaseStatements, defaultNornicDBStructEntityPhaseStatements),
		"Variable":    capOptionalBatchSize(entityPhaseStatements, defaultNornicDBVariableEntityPhaseStatements),
	}
}

func capOptionalBatchSize(configured int, limit int) int {
	if configured <= 0 || configured > limit {
		return limit
	}
	return configured
}

func nornicDBLabelSizeMap(
	getenv func(string) string,
	key string,
	defaults map[string]int,
	ceiling int,
) (map[string]int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaults, nil
	}
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("parse %s=%q: entries must be Label=size", key, raw)
		}
		label := strings.TrimSpace(parts[0])
		if label == "" {
			return nil, fmt.Errorf("parse %s=%q: label must be non-empty", key, raw)
		}
		size, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || size <= 0 {
			return nil, fmt.Errorf("parse %s=%q: label %q must have a positive integer size", key, raw, label)
		}
		defaults[label] = capOptionalBatchSize(ceiling, size)
	}
	return defaults, nil
}

func positiveOrDefault(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
