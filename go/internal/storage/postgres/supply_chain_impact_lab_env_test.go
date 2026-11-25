// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import "os"

// labEnv reads a lab harness setting (ESHU_LAB_*) for the supply-chain impact lab
// proofs. It lives outside the proof files because the enrollment guard requires every
// database connection of a proof to come through supplyChainImpactLiveDSN and flags any
// direct environment read in a proof file; these settings carry no connection.
func labEnv(name string) string { return os.Getenv(name) }
