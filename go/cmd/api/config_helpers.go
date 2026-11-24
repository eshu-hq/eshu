// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func envOrDefault(getenv func(string) string, key, fallback string) string {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func loadQueryProfile(getenv func(string) string) (query.QueryProfile, error) {
	raw := strings.TrimSpace(getenv("ESHU_QUERY_PROFILE"))
	if raw == "" {
		return query.ProfileProduction, nil
	}
	profile, err := query.ParseQueryProfile(raw)
	if err != nil {
		return "", err
	}
	return profile, nil
}

func loadGraphBackend(getenv func(string) string) (query.GraphBackend, error) {
	return query.ParseGraphBackend(strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")))
}

const defaultAPIShutdownTimeout = 30 * time.Second

// apiShutdownTimeout returns the graceful shutdown timeout for the API HTTP
// server. It reads ESHU_API_SHUTDOWN_TIMEOUT from the environment and parses it
// as a duration. If the variable is unset or invalid, the default of 30 s is
// returned. An explicit 5 s setting is honored for backwards compatibility with
// deployments that depend on the prior hard-coded value.
func apiShutdownTimeout(getenv func(string) string) time.Duration {
	if getenv == nil {
		return defaultAPIShutdownTimeout
	}
	raw := strings.TrimSpace(getenv("ESHU_API_SHUTDOWN_TIMEOUT"))
	if raw == "" {
		return defaultAPIShutdownTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultAPIShutdownTimeout
	}
	return d
}
