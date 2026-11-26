// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	impactdigest "github.com/eshu-hq/eshu/go/internal/ifa/impact/digest"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// impactDigestOptions holds the parsed inputs of one `ifa impact-digest` run.
type impactDigestOptions struct {
	postgresDSN string
	resweep     bool
	expect      string
	expectFile  string
}

func parseImpactDigestFlags(args []string, stderr io.Writer) (impactDigestOptions, error) {
	fs := flag.NewFlagSet("ifa impact-digest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o impactDigestOptions
	fs.StringVar(&o.postgresDSN, "postgres-dsn", "", "Postgres DSN to read (default: ESHU_POSTGRES_DSN/ESHU_FACT_STORE_DSN/ESHU_CONTENT_STORE_DSN)")
	fs.BoolVar(&o.resweep, "resweep", false, "run one full winners resweep before the read, as the winners maintainer does once the supports are written")
	fs.StringVar(&o.expect, "expect", "", "digest the run must equal; a mismatch fails")
	fs.StringVar(&o.expectFile, "expect-file", "", "pin file whose first non-comment line is the digest the run must equal (shared with the in-process family); a file that pins nothing is not a failure here, the cell requires the pin")
	if err := fs.Parse(args); err != nil {
		return impactDigestOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if o.expect != "" && o.expectFile != "" {
		return impactDigestOptions{}, fmt.Errorf("ifa impact-digest: -expect and -expect-file are exclusive")
	}
	return o, nil
}

// expectedImpactDigest returns the digest the run must equal, or "" when none is pinned.
func expectedImpactDigest(o impactDigestOptions) (string, error) {
	if o.expectFile == "" {
		return strings.TrimSpace(o.expect), nil
	}
	raw, err := os.ReadFile(o.expectFile)
	if err != nil {
		return "", fmt.Errorf("ifa impact-digest: read -expect-file: %w", err)
	}
	return impactdigest.Pinned(string(raw)), nil
}

// runImpactDigestCommand implements `ifa impact-digest`: the Postgres truth read of the supply-chain impact
// family (#10880 PR-C2). The impact domain writes no graph, so the Compose cell cannot compare a graph dump;
// it compares this digest. The verb optionally runs one winners resweep (the maintainer's own store port,
// retrying only the single-flight skip), reads the digest through impactdigest.Read (the statement the
// in-process family shares, refusing an empty family), prints it on one line and, when a digest is pinned,
// fails on a mismatch. The only write is the resweep, which is the production rebuild.
func runImpactDigestCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseImpactDigestFlags(args, stderr)
	if err != nil {
		return err
	}
	want, err := expectedImpactDigest(o)
	if err != nil {
		return err
	}
	db, err := driveOpenPostgres(ctx, o.postgresDSN)
	if err != nil {
		return fmt.Errorf("ifa impact-digest: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	if o.resweep {
		if _, _, _, _, err := rebuildWinnersOnce(ctx, postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})); err != nil {
			return fmt.Errorf("ifa impact-digest: winners resweep: %w", err)
		}
	}
	got, err := impactdigest.Read(ctx, db)
	if err != nil {
		return fmt.Errorf("ifa impact-digest: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "impact-digest: %s supports=%d winners=%d\n", got.Digest, got.Supports, got.Winners)
	if want != "" && want != got.Digest {
		return fmt.Errorf("ifa impact-digest: digest %s does not equal the pinned %s", got.Digest, want)
	}
	return nil
}
