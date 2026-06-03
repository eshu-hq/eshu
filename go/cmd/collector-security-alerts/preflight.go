package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
)

const preflightProviderAccessFlag = "--preflight-provider-access"

func hasProviderAccessPreflightFlag(args []string) bool {
	for _, arg := range args {
		if arg == preflightProviderAccessFlag {
			return true
		}
	}
	return false
}

func runProviderAccessPreflight(ctx context.Context, getenv func(string) string) error {
	return runProviderAccessPreflightWithFactory(ctx, getenv, nil)
}

func runProviderAccessPreflightWithFactory(
	ctx context.Context,
	getenv func(string) string,
	factory alertruntime.ClientFactory,
) error {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return err
	}
	config.Source.ClientFactory = factory
	source, err := alertruntime.NewClaimedSource(config.Source)
	if err != nil {
		return err
	}
	result, err := source.PreflightProviderAccess(ctx)
	if err != nil {
		return fmt.Errorf("security alert provider access preflight failed for %d target(s): %w", result.TargetCount, err)
	}
	return nil
}
