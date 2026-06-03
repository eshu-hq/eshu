package main

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive/vaultapi"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// buildCollectorService wires the read-only Vault metadata snapshot source onto
// the shared collector commit boundary.
func buildCollectorService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.Service{
		Source: &vaultlive.SnapshotSource{
			Config:        config.Collector,
			ClientFactory: vaultClientFactory{auth: config.Auth},
			Tracer:        tracer,
			Instruments:   instruments,
			Logger:        logger,
		},
		Committer:    committer,
		PollInterval: config.PollInterval,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}

// vaultAuth holds the read-only connection settings for one Vault target. The
// token is resolved from the environment (never from the targets JSON) so a
// secret never lands in serialized config.
type vaultAuth struct {
	Address   string
	Token     string
	Namespace string
}

// vaultClientFactory builds one read-only, metadata-only Vault adapter per
// configured target using the per-target connection settings. The auth map is
// keyed by (cluster, namespace) because the scope identity is namespace-scoped:
// one cluster may host multiple namespace targets with distinct tokens.
type vaultClientFactory struct {
	auth map[string]vaultAuth
}

// authKey is the (cluster, namespace) key shared by config parsing and the
// client factory so the namespace dimension of the scope identity is honored
// end to end.
func authKey(clusterID, namespace string) string {
	return clusterID + "\x00" + namespace
}

func (f vaultClientFactory) Client(_ context.Context, target vaultlive.ClusterTarget) (vaultlive.Client, error) {
	auth, ok := f.auth[authKey(target.VaultClusterID, target.Namespace)]
	if !ok {
		return nil, fmt.Errorf("no auth config for vault cluster %q namespace %q", target.VaultClusterID, target.Namespace)
	}
	client, err := vaultapi.New(vaultapi.Config{
		Address:   auth.Address,
		Token:     auth.Token,
		Namespace: auth.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("build vault client for cluster %q: %w", target.VaultClusterID, err)
	}
	return client, nil
}
