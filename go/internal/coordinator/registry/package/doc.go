// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packages plans workflow rows for configured and derived
// package-registry targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance,
// decodes its configured registry targets, derives additional targets from
// owned-package dependency evidence, ranks them by target class, and returns
// deterministic workflow rows without opening a registry connection. The
// parent coordinator package owns scheduling order, durable admission,
// persistence, retries, and telemetry.
//
// The directory is named package because it groups package-registry planning
// code; the package clause is packages because package is a reserved Go
// keyword. Importers need an explicit alias, for example:
//
//	packages "github.com/eshu-hq/eshu/go/internal/coordinator/registry/package"
package packages
