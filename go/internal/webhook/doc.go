// Package webhook verifies provider webhook authentication material and
// normalizes provider payloads into repository refresh trigger decisions.
//
// The package deliberately stops before persistence or queue handoff. Trigger
// describes the provider decision; StoredTrigger adds the durable status fields
// owned by storage implementations. A verified webhook is a wake-up signal for
// the normal Eshu collection flow, not graph truth and not a shortcut around
// repository snapshotting. Provider merge events without a merge commit and
// default-branch delete pushes are ignored rather than rewritten into another
// commit target.
package webhook
