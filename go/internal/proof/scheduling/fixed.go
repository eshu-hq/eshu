// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	fixedCaseTimeout  = 50 * time.Second
	fixedCloseTimeout = 10 * time.Second
)

func fixedCaseContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, fixedCaseTimeout)
}

func closeFixedReaders[T interface{ Close(context.Context) error }](readers []T) error {
	closeCtx, cancel := context.WithTimeout(context.Background(), fixedCloseTimeout)
	defer cancel()
	var cleanupErr error
	for index, reader := range readers {
		if err := reader.Close(closeCtx); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close fixed proof reader %d: %w", index, err))
		}
	}
	return cleanupErr
}

func selectFixedCanonical(name string) (dynamicWorkload, error) {
	return selectTimingWorkload(name, "")
}

func configureProofTarget(config *pgx.ConnConfig, mode, socketDir string) error {
	if config == nil {
		return fmt.Errorf("proof connection config is missing")
	}
	if socketDir != "" {
		if !fixedProofMode(mode) || !filepath.IsAbs(socketDir) {
			return fmt.Errorf("socket directory requires a fixed proof mode and an absolute path")
		}
		config.Host = socketDir
		config.Port = 5432
	} else {
		config.Host = "127.0.0.1"
		config.Port = 15433
	}
	config.TLSConfig = nil
	config.Fallbacks = nil
	return nil
}

func configureProofTargetPort(config *pgx.ConnConfig, mode, socketDir, fixedPort string) error {
	if fixedPort != "" && (!fixedProofMode(mode) || socketDir != "") {
		return fmt.Errorf("fixed proof port requires a fixed mode without a socket directory")
	}
	if err := configureProofTarget(config, mode, socketDir); err != nil {
		return err
	}
	if fixedPort == "" {
		return nil
	}
	port, err := strconv.ParseUint(fixedPort, 10, 16)
	if err != nil || port == 0 || strconv.FormatUint(port, 10) != fixedPort {
		return fmt.Errorf("fixed proof port must be a canonical nonzero TCP port")
	}
	config.Port = uint16(port)
	return nil
}

func fixedProofMode(mode string) bool {
	return mode == "fixed_canonical" || mode == "fixed_diagnostic"
}

func validateFixedDatabase(expected, configured string) error {
	if expected == "" || configured == "" || configured != expected {
		return fmt.Errorf("fixed proof database must be explicitly named and match connection config")
	}
	return nil
}

func validateExpectedSystemID(expected string) error {
	parsed, err := strconv.ParseUint(expected, 10, 64)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != expected {
		return fmt.Errorf("fixed proof requires explicit canonical PostgreSQL system identifier")
	}
	return nil
}

func requireFixedPrimary(expectedDatabase, actualDatabase, expectedSystemID, actualSystemID string, recovery bool, readOnly string) error {
	if err := validateFixedDatabase(expectedDatabase, actualDatabase); err != nil {
		return err
	}
	if err := validateExpectedSystemID(expectedSystemID); err != nil {
		return err
	}
	if actualSystemID != expectedSystemID {
		return fmt.Errorf("fixed proof PostgreSQL system identifier mismatch")
	}
	if recovery || readOnly != "on" {
		return fmt.Errorf("fixed proof requires read-only primary: recovery=%t transaction_read_only=%q", recovery, readOnly)
	}
	return nil
}

func runFixedCanonical(ctx context.Context, config *pgx.ConnConfig, expectedDatabase string) error {
	return runFixedProof(ctx, config, expectedDatabase, false)
}

func runFixedDiagnostic(ctx context.Context, config *pgx.ConnConfig, expectedDatabase string) error {
	return runFixedProof(ctx, config, expectedDatabase, true)
}

func runFixedProof(ctx context.Context, config *pgx.ConnConfig, expectedDatabase string, diagnostic bool) (resultErr error) {
	if config == nil {
		return fmt.Errorf("fixed proof connection config is missing")
	}
	if err := validateFixedDatabase(expectedDatabase, config.Database); err != nil {
		return err
	}
	expectedSystemID := os.Getenv("ESHU7033_EXPECTED_SYSTEM_ID")
	if err := validateExpectedSystemID(expectedSystemID); err != nil {
		return err
	}
	workload, err := selectFixedCanonical("canonical")
	if err != nil {
		return err
	}
	connections := make([]*pgx.Conn, 0, 4)
	defer func() {
		resultErr = errors.Join(resultErr, closeFixedReaders(connections))
	}()
	for range 4 {
		readerConfig := config.Copy()
		if readerConfig.RuntimeParams == nil {
			readerConfig.RuntimeParams = make(map[string]string)
		}
		readerConfig.RuntimeParams["default_transaction_read_only"] = "on"
		readerConfig.RuntimeParams["transaction_timeout"] = "50s"
		conn, err := pgx.ConnectConfig(ctx, readerConfig)
		if err != nil {
			return fmt.Errorf("connect fixed proof reader: %w", err)
		}
		connections = append(connections, conn)
		var actualDatabase, actualSystemID, readOnly string
		var recovery bool
		if err := conn.QueryRow(ctx, "SELECT current_database(), pg_is_in_recovery(), current_setting('transaction_read_only'), (SELECT system_identifier::text FROM pg_control_system())").Scan(&actualDatabase, &recovery, &readOnly, &actualSystemID); err != nil {
			return fmt.Errorf("verify fixed proof reader: %w", err)
		}
		if err := requireFixedPrimary(expectedDatabase, actualDatabase, expectedSystemID, actualSystemID, recovery, readOnly); err != nil {
			return err
		}
	}
	caseCtx, cancel := fixedCaseContext(ctx)
	defer cancel()
	if diagnostic {
		return runFixedDiagnosticCase(caseCtx, connections, workload, os.Stdout)
	}
	return runDynamicCase(caseCtx, connections, workload, true)
}
