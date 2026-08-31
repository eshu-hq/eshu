// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// sourceOrderKeyField forwards to [payloadcore.SourceOrderKeyField].
const sourceOrderKeyField = payloadcore.SourceOrderKeyField

// sourceOrderKey forwards to [payloadcore.SourceOrderKey].
func sourceOrderKey(env facts.Envelope) string {
	return payloadcore.SourceOrderKey(env)
}

// preferMaxSourceOrderKey forwards to [payloadcore.PreferMaxSourceOrderKey].
func preferMaxSourceOrderKey(existing, candidate map[string]any) bool {
	return payloadcore.PreferMaxSourceOrderKey(existing, candidate)
}
