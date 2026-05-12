// Package webhook verifies provider webhook authentication material and
// normalizes provider payloads into repository refresh trigger decisions.
//
// The package deliberately stops before persistence or queue handoff. A
// verified webhook is a wake-up signal for the normal Eshu collection flow, not
// graph truth and not a shortcut around repository snapshotting.
package webhook
