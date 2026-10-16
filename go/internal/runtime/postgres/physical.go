// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const physicalMetadataSQL = `SELECT pg_is_in_recovery()::text, current_setting('transaction_read_only'), current_setting('default_transaction_read_only'), system_identifier::text, current_database(), (extract(epoch from pg_postmaster_start_time())*1000000)::bigint::text, current_setting('server_version'), current_setting('server_version_num') FROM pg_control_system()`

// physicalIdentity is one session's physical metadata. serverVersion and
// serverVersionNum ride the same statement for the #7957 snapshot-export
// qualification; they are never part of the writer lineage or topology
// comparison, so a same-cluster minor upgrade stays a restart, not a new
// topology.
type physicalIdentity struct {
	recovery, readOnly, defaultReadOnly, systemID, database, incarnation, serverVersion string
	serverVersionNum                                                                    int
}

// parseServerVersionNum decodes current_setting('server_version_num'), which
// PostgreSQL always reports as a positive integer.
func parseServerVersionNum(value string) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return 0, errors.New("invalid PostgreSQL server_version_num")
	}
	return number, nil
}

func readPhysicalPGX(ctx context.Context, conn *pgx.Conn) (physicalIdentity, error) {
	var id physicalIdentity
	var versionNum string
	err := conn.QueryRow(ctx, physicalMetadataSQL).Scan(&id.recovery, &id.readOnly, &id.defaultReadOnly, &id.systemID, &id.database, &id.incarnation, &id.serverVersion, &versionNum)
	if err != nil {
		return id, err
	}
	id.serverVersionNum, err = parseServerVersionNum(versionNum)
	return id, err
}

func readPhysicalRaw(ctx context.Context, conn *pgconn.PgConn) (physicalIdentity, error) {
	results, err := conn.Exec(ctx, physicalMetadataSQL).ReadAll()
	if err != nil {
		return physicalIdentity{}, err
	}
	if len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) != 8 {
		return physicalIdentity{}, errors.New("invalid PostgreSQL identity metadata")
	}
	row := results[0].Rows[0]
	versionNum, err := parseServerVersionNum(string(row[7]))
	if err != nil {
		return physicalIdentity{}, errors.New("invalid PostgreSQL identity metadata")
	}
	return physicalIdentity{
		recovery: string(row[0]), readOnly: string(row[1]), defaultReadOnly: string(row[2]), systemID: string(row[3]),
		database: string(row[4]), incarnation: string(row[5]), serverVersion: string(row[6]), serverVersionNum: versionNum,
	}, nil
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
// observed first by a reader dial is published for the writer as well. An
// accepted dial refreshes the pool's snapshot-export verdict from the build it
// already read.
func samePrimaryReaderValidator(lineage *writerLineage, exportQualification *snapshotExportQualification) pgconn.ValidateConnectFunc {
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
		if err := lineage.validate(ctx, conn, base, id); err != nil {
			return err
		}
		exportQualification.observe(id.serverVersion, id.serverVersionNum, false)
		return nil
	}
}

// readerValidator guards a physical streaming standby and is the base check for
// each direct reader member. It compares no incarnation: a standby keeps its own
// postmaster through a primary restart, and the same-primary reader pool shares
// the writer lineage through samePrimaryReaderValidator instead. A non-nil
// exportQualification is the single reader pool's snapshot-export verdict,
// refreshed from each accepted dial; members pass nil and refresh their own
// verdict on every borrow instead.
func readerValidator(expected physicalIdentity, exportQualification *snapshotExportQualification) pgconn.ValidateConnectFunc {
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
		exportQualification.observe(id.serverVersion, id.serverVersionNum, true)
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
