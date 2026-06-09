// Package semanticpolicy evaluates hosted semantic extraction policy.
//
// The package is a pure contract layer: it parses source allowlists and
// semantic provider egress rules, validates scope and source-class policy, and
// returns reason-coded decisions without loading provider credentials, opening
// storage, or constructing prompts. Callers must pass fresh provider status and
// ACL state for the specific source; missing policy, unsupported source
// classes, stale ACLs, missing or denied egress, and unallowlisted scopes fail
// closed before provider work can be queued.
package semanticpolicy
