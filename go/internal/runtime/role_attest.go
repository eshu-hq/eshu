// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const attestConnectionSQL = `SELECT a.policy_revision::text, a.profile,
 a.session_role_oid::text, a.effective_role_oid::text, a.database_oid::text,
 a.database_identity::text, a.acl_inventory_version::text,
 session_user, current_user, current_database()
FROM eshu_security.attest_connection_v1($1,$2::bigint,$3,$4) AS a`

// Attest verifies the actual physical session against the bootstrap-owned
// role contract before a pool makes the connection available to work.
func (c *PostgresRoleContract) Attest(ctx context.Context, conn *pgconn.PgConn) error {
	if c == nil || conn == nil {
		return errors.New("postgres role attestation requires a contract and connection")
	}
	result := conn.ExecParams(ctx, attestConnectionSQL, [][]byte{
		[]byte(c.Route.ContractID),
		[]byte(strconv.FormatInt(c.PolicyRevision, 10)),
		[]byte(c.Workload),
		[]byte(c.RouteName),
	}, nil, nil, nil).Read()
	if result.Err != nil {
		var pgErr *pgconn.PgError
		if errors.As(result.Err, &pgErr) {
			return c.deny("checker_sqlstate_" + pgErr.Code)
		}
		return c.deny("checker_unavailable")
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 10 {
		return c.deny("checker_row_shape")
	}
	row := result.Rows[0]
	revision, revisionErr := strconv.ParseInt(string(row[0]), 10, 64)
	loginOID, loginErr := strconv.ParseUint(string(row[2]), 10, 32)
	effectiveOID, effectiveErr := strconv.ParseUint(string(row[3]), 10, 32)
	databaseOID, databaseErr := strconv.ParseUint(string(row[4]), 10, 32)
	databaseID, identityErr := uuid.Parse(string(row[5]))
	inventoryVersion, inventoryErr := strconv.ParseInt(string(row[6]), 10, 32)
	if revisionErr != nil || loginErr != nil || effectiveErr != nil || databaseErr != nil || identityErr != nil || inventoryErr != nil ||
		loginOID == 0 || effectiveOID == 0 || databaseOID == 0 || inventoryVersion <= 0 ||
		revision != c.PolicyRevision || string(row[1]) != c.Profile || databaseID.String() != c.DatabaseIdentity ||
		string(row[7]) != c.Route.SessionRole || string(row[8]) != c.Route.EffectiveRole || string(row[9]) != c.Database {
		return c.deny("checker_identity_mismatch")
	}
	slog.Info("postgres.role_attestation", "outcome", "accepted", "workload", c.Workload, "route", c.RouteName, "policy_revision", c.PolicyRevision)
	return nil
}

func (c *PostgresRoleContract) deny(reason string) error {
	slog.Error("postgres.role_attestation", "outcome", "denied", "reason", reason, "workload", c.Workload, "route", c.RouteName, "policy_revision", c.PolicyRevision)
	return fmt.Errorf("postgres role attestation denied for %s/%s (%s)", c.Workload, c.RouteName, reason)
}
