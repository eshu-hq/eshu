// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core_test

import (
	core "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The handler reaches the OS-package reader through a runtime type assertion
// (h.FactLoader.(osPackageAdvisoryFactLoader)). If postgres.FactStore stopped
// satisfying it the stage would be skipped silently, the pass would count as
// complete, and it would retract every OS-package finding. This assertion turns
// that drift into a compile error on every PR, not only in the scheduled live
// lane (#7154 review F4).
var _ core.OSPackageAdvisoryFactLoaderForTest = postgres.FactStore{}
