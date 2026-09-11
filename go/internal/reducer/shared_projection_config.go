// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"

// LoadSharedProjectionConfig forwards to [worker.LoadSharedProjectionConfig].
func LoadSharedProjectionConfig(getenv func(string) string) SharedProjectionRunnerConfig {
	return worker.LoadSharedProjectionConfig(getenv)
}
