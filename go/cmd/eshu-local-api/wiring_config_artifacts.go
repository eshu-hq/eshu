// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/content"
	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	storeartifacts "github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// configArtifactStoredAdapter adapts the storage read-model port to the
// query leaf's StoredReader port (#8155 PR3c). It reads through the given
// storage reader at the current extractor version and maps each answer 1:1:
// a ReasonTrusted repository is trusted, every other reason serves live with
// the storage reason as its reads-counter label, and the state's
// list_truncated rides along for the window reason.
type configArtifactStoredAdapter struct {
	reader storeartifacts.Reader
}

// newConfigArtifactStoredAdapter builds the production adapter over the
// replica queryer. A nil queryer fails every stored read, so the leaf serves
// everything live; startup still rejects a missing reader outright (see
// configureConfigArtifactReads).
func newConfigArtifactStoredAdapter(queryer db.Queryer) configArtifactStoredAdapter {
	return configArtifactStoredAdapter{reader: storeartifacts.NewPostgresReader(queryer)}
}

func (a configArtifactStoredAdapter) ReadStored(ctx context.Context, repoIDs []string) ([]artifacts.StoredRepository, error) {
	repos, err := a.reader.ReadStored(ctx, repoIDs, contentartifacts.ExtractorVersion)
	if err != nil {
		return nil, err
	}
	out := make([]artifacts.StoredRepository, 0, len(repos))
	for _, repo := range repos {
		stored := artifacts.StoredRepository{
			RepoID:        repo.RepoID,
			Trusted:       repo.Reason == storeartifacts.ReasonTrusted,
			Reason:        string(repo.Reason),
			ListTruncated: repo.ListTruncated,
		}
		for _, row := range repo.Rows {
			stored.Rows = append(stored.Rows, contentartifacts.StoredRow{
				RawPath:      row.RawPath,
				RelativePath: row.RelativePath,
				EvidenceKind: row.EvidenceKind,
				NameScoped:   row.NameScoped,
			})
		}
		out = append(out, stored)
	}
	return out, nil
}

// configureConfigArtifactReads applies ESHU_CONFIG_ARTIFACT_READ_MODE at API
// start (#8155 PR3c). live (the default) changes nothing. shadow serves the
// live answer while the production reporter counts the compare and warns on
// a mismatch; stored serves trusted stored rows with a live fallback per
// repository. An unknown mode fails startup, as does shadow or stored with
// no Postgres reader: silently serving live under a stored flag would hide
// the cutover. Rollback stays a flag flip to live with an API roll.
func configureConfigArtifactReads(getenv func(string) string, reader db.Queryer, instruments *telemetry.Instruments, logger *slog.Logger) error {
	cfg, err := content.LoadConfigArtifactReadMode(getenv)
	if err != nil {
		return err
	}
	if cfg.Mode == content.ReadModeLive {
		return nil
	}
	if reader == nil {
		return fmt.Errorf("config artifact read mode %q needs a Postgres read store", string(cfg.Mode))
	}
	adapter := newConfigArtifactStoredAdapter(reader)
	if cfg.Mode == content.ReadModeShadow {
		artifacts.ConfigureProductionShadowReads(adapter, func(ctx context.Context, report artifacts.ShadowReport) {
			repository.ReportConfigArtifactShadow(ctx, instruments, logger, report)
		})
	} else {
		artifacts.ConfigureProductionStoredReads(adapter)
	}
	if logger != nil {
		logger.Info("config-artifact read mode selected",
			telemetry.EventAttr("config_artifacts.read_mode"),
			slog.String("read_mode", string(cfg.Mode)))
	}
	return nil
}
