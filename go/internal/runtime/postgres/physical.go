// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const physicalMetadataSQL = `SELECT pg_is_in_recovery()::text, current_setting('transaction_read_only'), current_setting('default_transaction_read_only'), system_identifier::text, current_database(), (extract(epoch from pg_postmaster_start_time())*1000000)::bigint::text FROM pg_control_system()`

type physicalIdentity struct{ recovery, readOnly, defaultReadOnly, systemID, database, incarnation string }

func readPhysicalPGX(ctx context.Context, conn *pgx.Conn) (physicalIdentity, error) {
	var id physicalIdentity
	err := conn.QueryRow(ctx, physicalMetadataSQL).Scan(&id.recovery, &id.readOnly, &id.defaultReadOnly, &id.systemID, &id.database, &id.incarnation)
	return id, err
}

func readPhysicalRaw(ctx context.Context, conn *pgconn.PgConn) (physicalIdentity, error) {
	results, err := conn.Exec(ctx, physicalMetadataSQL).ReadAll()
	if err != nil {
		return physicalIdentity{}, err
	}
	if len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) != 6 {
		return physicalIdentity{}, errors.New("invalid PostgreSQL identity metadata")
	}
	row := results[0].Rows[0]
	return physicalIdentity{string(row[0]), string(row[1]), string(row[2]), string(row[3]), string(row[4]), string(row[5])}, nil
}

func writerValidator(expected physicalIdentity) pgconn.ValidateConnectFunc {
	return func(ctx context.Context, conn *pgconn.PgConn) error {
		if err := pgconn.ValidateConnectTargetSessionAttrsPrimary(ctx, conn); err != nil {
			return fmt.Errorf("writer physical role: %w", err)
		}
		if err := pgconn.ValidateConnectTargetSessionAttrsReadWrite(ctx, conn); err != nil {
			return fmt.Errorf("writer writable session: %w", err)
		}
		id, err := readPhysicalRaw(ctx, conn)
		if err != nil {
			return fmt.Errorf("writer metadata: %w", err)
		}
		if id.recovery != "false" || id.readOnly != "off" || id.defaultReadOnly != "off" || (expected.systemID != "" && id.systemID != expected.systemID) || (expected.database != "" && id.database != expected.database) || (expected.incarnation != "" && id.incarnation != expected.incarnation) {
			return ErrWrongTopology
		}
		return nil
	}
}

func readerValidator(expected physicalIdentity, samePrimary bool) pgconn.ValidateConnectFunc {
	return func(ctx context.Context, conn *pgconn.PgConn) error {
		if samePrimary {
			if err := pgconn.ValidateConnectTargetSessionAttrsPrimary(ctx, conn); err != nil {
				return fmt.Errorf("reader physical role: %w", err)
			}
		} else {
			if err := pgconn.ValidateConnectTargetSessionAttrsStandby(ctx, conn); err != nil {
				return fmt.Errorf("reader physical role: %w", err)
			}
		}
		id, err := readPhysicalRaw(ctx, conn)
		if err != nil {
			return fmt.Errorf("reader metadata: %w", err)
		}
		expectedRecovery := "true"
		if samePrimary {
			expectedRecovery = "false"
		}
		if id.recovery != expectedRecovery || id.defaultReadOnly != "on" || id.systemID != expected.systemID || id.database != expected.database || (samePrimary && id.incarnation != expected.incarnation) {
			return ErrWrongTopology
		}
		return nil
	}
}

func bootstrapPhysicalWriter(ctx context.Context, cfg *pgx.ConnConfig, expectedSystemID string) (physicalIdentity, error) {
	expected := physicalIdentity{systemID: expectedSystemID, database: cfg.Database}
	cfg.ValidateConnect = writerValidator(expected)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return physicalIdentity{}, fmt.Errorf("writer bootstrap: %w", err)
	}
	id, readErr := readPhysicalPGX(ctx, conn)
	closeErr := conn.Close(ctx)
	if readErr != nil {
		return physicalIdentity{}, fmt.Errorf("writer bootstrap metadata: %w", readErr)
	}
	if closeErr != nil {
		return physicalIdentity{}, fmt.Errorf("writer bootstrap close: %w", closeErr)
	}
	if id.recovery != "false" || id.readOnly != "off" || id.defaultReadOnly != "off" || id.systemID == "" || id.database != cfg.Database || id.incarnation == "" || (expectedSystemID != "" && id.systemID != expectedSystemID) {
		return physicalIdentity{}, ErrWrongTopology
	}
	return id, nil
}
