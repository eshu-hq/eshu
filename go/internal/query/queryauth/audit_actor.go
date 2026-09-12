// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// GovernanceAuditAppender records validation-safe governance audit events.
// Moved from root package query's auth.go (#6642) so a handler-family
// subpackage can accept an audit appender without importing root.
type GovernanceAuditAppender interface {
	Append(context.Context, []governanceaudit.Event) error
}

// ActorClassForAuth maps the credential a caller presented to the
// governance audit actor class every emitter in root package query stamps:
// a cookie session is browser_session, a scoped or OIDC bearer is
// scoped_token, and the legacy shared bearer is shared_token. Route
// denials, allowed reads, identity mutations, and admin recovery actions
// all use it, so one credential maps to one class across the audit
// vocabulary and an operator filtering by actor_class sees one population
// per credential (#6566). governanceaudit.ActorClassOperator is not
// produced here: it is reserved for a human asserting an identity through
// an SSO login, which carries no AuthContext.
//
// Callers own the subject-hash rule, and each emitter keeps the rule it had
// before #6566. Every class this returns except anonymous is
// identity-bearing. Route denials, allowed reads, and admin recovery
// downgrade to anonymous when the hash is blank.
//
// The switch names every AuthMode member and has no default on purpose. A
// default would quietly file a mode added later under whichever class it
// named, and the audit row would then lie about who acted; with no default
// the exhaustive linter fails the build until the new mode is given a class
// of its own. Only a blank mode reaches the final return: an AuthContext
// nobody authenticated, and that is anonymous.
//
// Moved from root package query's auth_audit.go (actorClassForAuth, #6642)
// so a handler-family subpackage can classify an AuthContext for its own
// audit rows without importing root.
func ActorClassForAuth(auth AuthContext) governanceaudit.ActorClass {
	switch auth.Mode {
	case AuthModeBrowserSession:
		return governanceaudit.ActorClassBrowserSession
	case AuthModeScoped:
		return governanceaudit.ActorClassScopedToken
	case AuthModeShared:
		return governanceaudit.ActorClassSharedToken
	}
	return governanceaudit.ActorClassAnonymous
}
