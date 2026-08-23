// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azure

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
)

// ReducerIntent preserves the moved builders' value spelling while the family
// extraction lands.
type ReducerIntent = projectorintent.ReducerIntent

func cloudInventoryAdmissionSourceSystem(envelope facts.Envelope) string {
	return projectorintent.SourceSystem(envelope)
}
