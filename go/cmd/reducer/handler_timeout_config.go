// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

const (
	reducerHandlerTimeoutEnv          = "ESHU_REDUCER_HANDLER_TIMEOUT"
	reducerHandlerTimeoutOverridesEnv = "ESHU_REDUCER_HANDLER_TIMEOUT_OVERRIDES"
)

// loadReducerHandlerTimeout reads the default bound on one handler execution.
// Empty means no bound is configured. An explicit zero duration ("0s")
// disables the bound: it opts out loudly, distinct from an empty value the
// operator may never have set. An unparsable or negative value is an error:
// silently running unbounded on a typo would hide a misconfigured bound, so
// startup refuses it like the overrides path does. A positive value is
// enforced by the reducer service (#10866): the worker is freed when the
// bound expires and the intent fails with the counting
// reducer_handler_timeout class.
func loadReducerHandlerTimeout(getenv func(string) string) (time.Duration, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv(reducerHandlerTimeoutEnv))
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must carry a positive duration or 0s to disable, got %q", reducerHandlerTimeoutEnv, raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must carry a positive duration or 0s to disable, got %q", reducerHandlerTimeoutEnv, raw)
	}
	return value, nil
}

// loadReducerHandlerTimeoutOverrides parses per-domain handler bounds in
// "domain=duration,domain=duration" form. An empty value means no overrides.
// An explicit "domain=0s" exempts that domain from the default bound
// (HandlerTimeoutFor returns a present 0 as-is). Unknown domains, negative
// durations, and unparsable values are errors: a bound that names nothing
// would silently never apply, which is worse than refusing to start.
func loadReducerHandlerTimeoutOverrides(getenv func(string) string) (map[reducercontract.Domain]time.Duration, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv(reducerHandlerTimeoutOverridesEnv))
	if raw == "" {
		return nil, nil
	}
	overrides := make(map[reducercontract.Domain]time.Duration)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("%s contains an empty entry", reducerHandlerTimeoutOverridesEnv)
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("%s entry %q must be domain=duration", reducerHandlerTimeoutOverridesEnv, part)
		}
		domain, err := reducer.ParseDomain(strings.TrimSpace(name))
		if err != nil {
			return nil, fmt.Errorf("%s entry %q names an unknown reducer domain: %w", reducerHandlerTimeoutOverridesEnv, part, err)
		}
		bound, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || bound < 0 {
			return nil, fmt.Errorf("%s entry %q must carry a positive duration or 0s to exempt the domain", reducerHandlerTimeoutOverridesEnv, part)
		}
		if _, dup := overrides[domain]; dup {
			return nil, fmt.Errorf("%s names domain %q twice; a repeated domain is a copy-paste mistake, not an override", reducerHandlerTimeoutOverridesEnv, name)
		}
		overrides[domain] = bound
	}
	return overrides, nil
}

// handlerTimeoutsEnforced reports whether any bound actually constrains a
// handler: a positive default, or any positive per-domain override. An
// all-zero configuration (including a map of pure 0s exemptions) leaves
// every domain unbounded, so the startup log must say disabled, not
// enforced.
func handlerTimeoutsEnforced(def time.Duration, overrides map[reducercontract.Domain]time.Duration) bool {
	if def > 0 {
		return true
	}
	for _, bound := range overrides {
		if bound > 0 {
			return true
		}
	}
	return false
}
