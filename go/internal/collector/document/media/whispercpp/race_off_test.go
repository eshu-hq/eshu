// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !race

package whispercpp

// raceEnabled reports that the test binary, which doubles as the fake
// engine, carries the race detector's large shadow-memory mappings.
const raceEnabled = false
