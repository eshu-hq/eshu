// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "github.com/eshu-hq/eshu/go/internal/query/ask"

func init() {
	register(askNaturalLanguageAnswerCapability, ask.Support())
}
