// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// PostgresWorkload is the compiled workload class used when asking the
// database to attest a physical connection. It never comes from pod JSON.
type PostgresWorkload string

// Compiled workload classes bind each binary to its permitted DB route.
const (
	PostgresWorkloadRuntime          PostgresWorkload = "runtime"
	PostgresWorkloadAPI              PostgresWorkload = "api"
	PostgresWorkloadMCP              PostgresWorkload = "mcp-server"
	PostgresWorkloadCoordinator      PostgresWorkload = "workflow-coordinator"
	PostgresWorkloadBootstrap        PostgresWorkload = "schema-bootstrap"
	PostgresWorkloadAdvisoryProducer PostgresWorkload = "collector-vulnerability-intelligence"
)

// PostgresRoute identifies a compiled physical pool route.
type PostgresRoute string

// Physical routes have fixed SQL privilege profiles; pod JSON supplies only
// expected identities and a contract reference.
const (
	PostgresRouteWriter    PostgresRoute = "writer"
	PostgresRouteReader    PostgresRoute = "reader"
	PostgresRouteControl   PostgresRoute = "control"
	PostgresRouteBootstrap PostgresRoute = "bootstrap"
	PostgresRouteProducer  PostgresRoute = "producer"
)

// PostgresRoleRouteAssertion carries expected identity for one pool route.
// The database's protected contract remains the source of authorization.
type PostgresRoleRouteAssertion struct {
	ContractID    string `json:"contract_id"`
	SessionRole   string `json:"session_role"`
	EffectiveRole string `json:"effective_role"`
}

// PostgresRoleContract is a pod's expected revision and DB identity. Attest
// checks the active bootstrap-owned registry against the actual connection.
type PostgresRoleContract struct {
	PolicyRevision   int64
	DatabaseIdentity string
	Database         string
	Workload         PostgresWorkload
	RouteName        PostgresRoute
	Profile          string
	Route            PostgresRoleRouteAssertion
}

type postgresRoleContractJSON struct {
	PolicyRevision   int64                                        `json:"policy_revision"`
	DatabaseIdentity string                                       `json:"database_identity"`
	Database         string                                       `json:"database"`
	Routes           map[PostgresRoute]PostgresRoleRouteAssertion `json:"routes"`
}

// LoadPostgresRoleContract parses one route's assertions. An unset value is
// the legacy deployment contract; callers enabling authority must reject it.
func LoadPostgresRoleContract(getenv func(string) string, workload PostgresWorkload, route PostgresRoute) (*PostgresRoleContract, error) {
	if getenv == nil {
		return nil, fmt.Errorf("postgres role contract environment reader is required")
	}
	profile, ok := postgresRouteProfile(workload, route)
	if !ok {
		return nil, fmt.Errorf("postgres workload/route is not permitted")
	}
	required := strings.TrimSpace(getenv("ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED"))
	if required != "" && required != "true" {
		return nil, fmt.Errorf("ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED must be true or unset")
	}
	raw := strings.TrimSpace(getenv("ESHU_POSTGRES_ROLE_CONTRACT_JSON"))
	if raw == "" {
		if required == "true" {
			return nil, fmt.Errorf("ESHU_POSTGRES_ROLE_CONTRACT_JSON is required by the Postgres role boundary")
		}
		return nil, nil
	}
	var value postgresRoleContractJSON
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("ESHU_POSTGRES_ROLE_CONTRACT_JSON is invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("ESHU_POSTGRES_ROLE_CONTRACT_JSON contains trailing data")
	}
	identity, err := uuid.Parse(value.DatabaseIdentity)
	if value.PolicyRevision <= 0 || err != nil || identity == uuid.Nil || strings.TrimSpace(value.Database) == "" {
		return nil, fmt.Errorf("ESHU_POSTGRES_ROLE_CONTRACT_JSON has invalid revision or database identity")
	}
	assertion, found := value.Routes[route]
	if !found || strings.TrimSpace(assertion.ContractID) == "" || strings.TrimSpace(assertion.SessionRole) == "" || strings.TrimSpace(assertion.EffectiveRole) == "" {
		return nil, fmt.Errorf("ESHU_POSTGRES_ROLE_CONTRACT_JSON lacks the required route assertion")
	}
	return &PostgresRoleContract{PolicyRevision: value.PolicyRevision, DatabaseIdentity: identity.String(), Database: value.Database, Workload: workload, RouteName: route, Profile: profile, Route: assertion}, nil
}

func postgresRouteProfile(workload PostgresWorkload, route PostgresRoute) (string, bool) {
	switch workload {
	case PostgresWorkloadRuntime:
		return "runtime-writer", route == PostgresRouteWriter
	case PostgresWorkloadAPI, PostgresWorkloadMCP:
		if route == PostgresRouteWriter {
			return "runtime-writer", true
		}
		return "runtime-reader", route == PostgresRouteReader
	case PostgresWorkloadCoordinator:
		return "authority-control", route == PostgresRouteControl
	case PostgresWorkloadBootstrap:
		return "schema-bootstrap", route == PostgresRouteBootstrap
	case PostgresWorkloadAdvisoryProducer:
		return "typed-source-producer", route == PostgresRouteProducer
	default:
		return "", false
	}
}
