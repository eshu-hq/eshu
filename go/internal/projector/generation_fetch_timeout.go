// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "errors"

// ErrGenerationFetchTimeout fails a projector generation-fact load
// when a bounded fact page dies to a statement timeout (SQLSTATE 57014) or a
// context deadline (#11432). A page that dies partway is a failed fetch,
// never a short success with the rows read so far: the next attempt re-reads
// the generation from its first page, so the failure stays retryable through
// the normal Fail path instead of projecting a partial generation.
var ErrGenerationFetchTimeout = errors.New("projector generation fact fetch timed out")
