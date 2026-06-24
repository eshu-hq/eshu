// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import "fmt"

func phpAnonymousClassName(lineNumber int) string {
	return fmt.Sprintf("anonymous_class_%d", lineNumber)
}
