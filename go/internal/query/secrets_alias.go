// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: type aliases and thin forwarders for the moved secrets/IAM family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/secrets"
)

// secrets_alias.go is the root alias shim for the secrets/IAM handler family
// (#6642, modelled on service_alias.go and admin_alias.go).
// SecretsIAMHandler and its method files moved to secrets/. Names the rest of
// the program still spells `query.X` (handler wiring, cmd routers, staying
// root callers and tests) alias here so the move touches no caller outside
// the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import secrets directly.

// SecretsIAMHandler is the secrets/IAM handler family type. Its home is
// secrets/; this alias keeps cmd/api's and cmd/mcp-server's wiring_router.go
// struct literals and staying root tests spelling query.SecretsIAMHandler
// unchanged.
type SecretsIAMHandler = secrets.Handler

// The five secrets/IAM capability consts below preserve the unexported root
// spellings contract_secrets_iam.go's init() (Part C, not touched by this
// move) reads to register the capability matrix rows. Their home is secrets/,
// exported there as the five IAM*Capability consts because this alias file
// and this package's tests are the callers that need the package-local name.
const (
	secretsIAMIdentityTrustChainsCapability          = secrets.IAMIdentityTrustChainsCapability
	secretsIAMPrivilegePostureObservationsCapability = secrets.IAMPrivilegePostureObservationsCapability
	secretsIAMSecretAccessPathsCapability            = secrets.IAMSecretAccessPathsCapability
	secretsIAMPostureGapsCapability                  = secrets.IAMPostureGapsCapability
	secretsIAMPostureSummaryCapability               = secrets.IAMPostureSummaryCapability
)

// secretsIAMQueryer mirrors the bounded read surface (secretsIAMReadQueryer /
// secretsIAMIdentityTrustChainQueryer) the five Postgres store constructors
// below require. It is declared locally, not imported, because both leaf
// interfaces are unexported: cmd/api's and cmd/mcp-server's wiring pass a
// *sql.DB, which satisfies this shape and therefore the leaf's identical
// unexported interfaces structurally.
type secretsIAMQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// NewPostgresSecretsIAMIdentityTrustChainStore forwards to
// secrets.NewPostgresIAMIdentityTrustChainStore. Its home is secrets/; this
// wrapper keeps cmd/api's and cmd/mcp-server's wiring calling the
// package-local name unchanged.
func NewPostgresSecretsIAMIdentityTrustChainStore(db secretsIAMQueryer) secrets.PostgresIAMIdentityTrustChainStore {
	return secrets.NewPostgresIAMIdentityTrustChainStore(db)
}

// NewPostgresSecretsIAMPrivilegePostureObservationStore forwards to
// secrets.NewPostgresIAMPrivilegePostureObservationStore. Its home is
// secrets/; this wrapper keeps cmd/api's and cmd/mcp-server's wiring calling
// the package-local name unchanged.
func NewPostgresSecretsIAMPrivilegePostureObservationStore(db secretsIAMQueryer) secrets.PostgresIAMPrivilegePostureObservationStore {
	return secrets.NewPostgresIAMPrivilegePostureObservationStore(db)
}

// NewPostgresSecretsIAMSecretAccessPathStore forwards to
// secrets.NewPostgresIAMSecretAccessPathStore. Its home is secrets/; this
// wrapper keeps cmd/api's and cmd/mcp-server's wiring calling the
// package-local name unchanged.
func NewPostgresSecretsIAMSecretAccessPathStore(db secretsIAMQueryer) secrets.PostgresIAMSecretAccessPathStore {
	return secrets.NewPostgresIAMSecretAccessPathStore(db)
}

// NewPostgresSecretsIAMPostureGapStore forwards to
// secrets.NewPostgresIAMPostureGapStore. Its home is secrets/; this wrapper
// keeps cmd/api's and cmd/mcp-server's wiring calling the package-local name
// unchanged.
func NewPostgresSecretsIAMPostureGapStore(db secretsIAMQueryer) secrets.PostgresIAMPostureGapStore {
	return secrets.NewPostgresIAMPostureGapStore(db)
}

// NewPostgresSecretsIAMPostureSummaryStore forwards to
// secrets.NewPostgresIAMPostureSummaryStore. Its home is secrets/; this
// wrapper keeps cmd/api's and cmd/mcp-server's wiring calling the
// package-local name unchanged.
func NewPostgresSecretsIAMPostureSummaryStore(db secretsIAMQueryer) secrets.PostgresIAMPostureSummaryStore {
	return secrets.NewPostgresIAMPostureSummaryStore(db)
}

// NewGraphSecretsIAMGrantPostureStore forwards to
// secrets.NewGraphIAMGrantPostureStore. Its home is secrets/; this wrapper
// keeps cmd/api's and cmd/mcp-server's wiring calling the package-local name
// unchanged.
func NewGraphSecretsIAMGrantPostureStore(graph GraphQuery) secrets.GraphIAMGrantPostureStore {
	return secrets.NewGraphIAMGrantPostureStore(graph)
}
