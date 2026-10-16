// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import "strings"

// EnabledEnv names the switch of the changed-since link ledger (#7127). The
// reducer reads it to run the link runner; the ingester and bootstrap-index
// read it to make the projector Ack journal the activations it commits.
const EnabledEnv = "ESHU_CHANGED_SINCE_LINK_ENABLED"

// Enabled reports whether getenv turns the ledger switch on. Every binary
// parses the switch here, so the link runner and the Ack journal agree on
// any value: 1, true, t, yes, y and on (any case, surrounding blanks
// trimmed) are on; anything else, an unparsable value included, is off.
func Enabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(EnabledEnv))) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	}
	return false
}
