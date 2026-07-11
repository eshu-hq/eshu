// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildReducerServiceRejectsDisabledGenerationRetentionInProduction(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	_, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(key string) string {
			if key == generationRetentionEnabledEnv {
				return "false"
			}
			return ""
		},
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("buildReducerService() error = nil, want production retention disable rejected")
	}
}

func TestBuildReducerServiceAllowsDisabledGenerationRetentionForLocalProfiles(t *testing.T) {
	t.Parallel()

	for _, profile := range []query.QueryProfile{
		query.ProfileLocalLightweight,
		query.ProfileLocalAuthoritative,
		query.ProfileLocalFullStack,
	} {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()

			db := &fakeReducerDB{}
			service, err := buildReducerService(
				context.Background(),
				db,
				stubGraphExecutor{},
				stubCypherExecutor{},
				postgres.NewSharedIntentStore(db),
				stubCypherReader{},
				stubCypherReader{},
				func(key string) string {
					switch key {
					case generationRetentionEnabledEnv:
						return "false"
					case queryProfileEnv:
						return string(profile)
					default:
						return ""
					}
				},
				nil,
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("buildReducerService() error = %v, want nil for explicit local disable", err)
			}
			if service.GenerationRetentionRunner != nil {
				t.Fatal("GenerationRetentionRunner = non-nil, want local disable honored")
			}
		})
	}
}

func TestBuildReducerServiceRejectsDisabledGenerationRetentionWithProductionProfile(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	_, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(key string) string {
			switch key {
			case generationRetentionEnabledEnv:
				return "false"
			case queryProfileEnv:
				return string(query.ProfileProduction)
			default:
				return ""
			}
		},
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("buildReducerService() error = nil, want production profile rejected")
	}
}

func TestBuildReducerServiceRejectsDisabledGenerationRetentionWithInvalidProfile(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	_, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(key string) string {
			switch key {
			case generationRetentionEnabledEnv:
				return "false"
			case queryProfileEnv:
				return "definitely-not-a-profile"
			default:
				return ""
			}
		},
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("buildReducerService() error = nil, want invalid profile rejected")
	}
}
