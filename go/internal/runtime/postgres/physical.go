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

// readWriterIdentity proves a writable primary session and reads its shared
// physical metadata.
func readWriterIdentity(ctx context.Context, conn *pgconn.PgConn) (physicalIdentity, error) {
	if err := pgconn.ValidateConnectTargetSessionAttrsPrimary(ctx, conn); err != nil {
		return physicalIdentity{}, fmt.Errorf("writer physical role: %w", err)
	}
	if err := pgconn.ValidateConnectTargetSessionAttrsReadWrite(ctx, conn); err != nil {
		return physicalIdentity{}, fmt.Errorf("writer writable session: %w", err)
	}
	id, err := readPhysicalRaw(ctx, conn)
	if err != nil {
		return physicalIdentity{}, fmt.Errorf("writer metadata: %w", err)
	}
	if id.recovery != "false" || id.readOnly != "off" || id.defaultReadOnly != "off" {
		return physicalIdentity{}, ErrWrongTopology
	}
	return id, nil
}

// bootstrapWriterValidator accepts the first writer before any identity is
// published: a writable primary with the expected system and database.
func bootstrapWriterValidator(systemID, database string) pgconn.ValidateConnectFunc {
	return func(ctx context.Context, conn *pgconn.PgConn) error {
		id, err := readWriterIdentity(ctx, conn)
		if err != nil {
			return err
		}
		if (systemID != "" && id.systemID != systemID) || (database != "" && id.database != database) {
			return ErrWrongTopology
		}
		return nil
	}
}

// writerValidator accepts a writer connection on the published identity with
// one metadata query, and re-bootstraps a same-lineage primary restart.
func writerValidator(lineage *writerLineage) pgconn.ValidateConnectFunc {
	return func(ctx context.Context, conn *pgconn.PgConn) error {
		if lineage.isLatched() {
			return ErrWrongTopology
		}
		base := lineage.identity()
		id, err := readWriterIdentity(ctx, conn)
		if err != nil {
			return err
		}
		return lineage.validate(ctx, conn, base, id)
	}
}

// samePrimaryReaderValidator guards the read-only session pool that shares the
// writer's primary. It shares the writer lineage, so a same-cluster restart
// observed first by a reader dial is published for the writer as well.
func samePrimaryReaderValidator(lineage *writerLineage) pgconn.ValidateConnectFunc {
	return func(ctx context.Context, conn *pgconn.PgConn) error {
		if lineage.isLatched() {
			return ErrWrongTopology
		}
		base := lineage.identity()
		if err := pgconn.ValidateConnectTargetSessionAttrsPrimary(ctx, conn); err != nil {
			return fmt.Errorf("reader physical role: %w", err)
		}
		id, err := readPhysicalRaw(ctx, conn)
		if err != nil {
			return fmt.Errorf("reader metadata: %w", err)
		}
		if id.recovery != "false" || id.defaultReadOnly != "on" {
			return ErrWrongTopology
		}
		return lineage.validate(ctx, conn, base, id)
	}
}

// readerValidator guards a physical streaming standby and is the base check for
// each direct reader member. It compares no incarnation: a standby keeps its own
// postmaster through a primary restart, and the same-primary reader pool shares
// the writer lineage through samePrimaryReaderValidator instead.
func readerValidator(expected physicalIdentity) pgconn.ValidateConnectFunc {
	return func(ctx context.Context, conn *pgconn.PgConn) error {
		if err := pgconn.ValidateConnectTargetSessionAttrsStandby(ctx, conn); err != nil {
			return fmt.Errorf("reader physical role: %w", err)
		}
		id, err := readPhysicalRaw(ctx, conn)
		if err != nil {
			return fmt.Errorf("reader metadata: %w", err)
		}
		if id.recovery != "true" || id.defaultReadOnly != "on" || id.systemID != expected.systemID || id.database != expected.database {
			return ErrWrongTopology
		}
		return nil
	}
}

// bootstrapPhysicalWriter validates the first writer and reads its identity,
// insert timeline, and flushed WAL position on one connection. The flushed
// position seeds the restart watermark before any checkpoint runs.
func bootstrapPhysicalWriter(ctx context.Context, cfg *pgx.ConnConfig, expectedSystemID string) (physicalIdentity, lineageObservation, error) {
	cfg.ValidateConnect = bootstrapWriterValidator(expectedSystemID, cfg.Database)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return physicalIdentity{}, lineageObservation{}, fmt.Errorf("writer bootstrap: %w", err)
	}
	id, readErr := readPhysicalPGX(ctx, conn)
	var observed lineageObservation
	if readErr == nil {
		observed, readErr = readLineagePGX(ctx, conn)
	}
	closeErr := conn.Close(ctx)
	if readErr != nil {
		return physicalIdentity{}, lineageObservation{}, fmt.Errorf("writer bootstrap metadata: %w", readErr)
	}
	if closeErr != nil {
		return physicalIdentity{}, lineageObservation{}, fmt.Errorf("writer bootstrap close: %w", closeErr)
	}
	if id.recovery != "false" || id.readOnly != "off" || id.defaultReadOnly != "off" || id.systemID == "" || id.database != cfg.Database || id.incarnation == "" || (expectedSystemID != "" && id.systemID != expectedSystemID) {
		return physicalIdentity{}, lineageObservation{}, ErrWrongTopology
	}
	return id, observed, nil
}
