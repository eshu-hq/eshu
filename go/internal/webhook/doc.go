// Package webhook verifies GitHub, GitLab, and Bitbucket webhook authentication
// material and normalizes provider payloads into repository refresh trigger
// decisions.
//
// The package deliberately stops before persistence or queue handoff. Trigger
// describes the provider decision; StoredTrigger adds the durable status fields
// owned by storage implementations. A verified webhook is a wake-up signal for
// the normal Eshu collection flow, not graph truth and not a shortcut around
// repository snapshotting. Provider merge events without a merge commit and
// default-branch delete pushes are ignored rather than rewritten into another
// commit target. Merged GitHub pull-request events also carry bounded
// pull-request number, URL, and title provenance so read models can connect a
// refreshed commit to provider-owned PR evidence without treating the webhook
// as graph truth.
package webhook
