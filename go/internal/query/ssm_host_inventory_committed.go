// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

type ssmCommittedHost struct {
	host       cloud.SSMHostTuple
	apps       []map[string]any
	appUIDs    map[string]bool
	platforms  map[string]map[string]any
	scopeID    string
	generation string
}

// readSSMCommittedSeeds returns the committed host keys in HostKey order,
// truncated to the page prefix: application-seeded hosts plus platform-only
// hosts (platform seeds minus application seeds). Both seed legs arrive
// prefix-first in tuple order, so the union keeps the committed prefix.
func readSSMCommittedSeeds(ctx context.Context, graph GraphQuery, filter ssmHostInventoryFilter, prefix int) ([]string, error) {
	if graph == nil {
		return nil, nil
	}
	params := map[string]any{
		"all_scopes":  filter.AllScopes,
		"scope_ids":   append([]string{}, append(filter.AllowedScopeIDs, filter.AllowedRepositoryIDs...)...),
		"instance_id": filter.InstanceID,
		"account_id":  filter.AccountID,
		"region":      filter.Region,
		"seed_limit":  prefix,
	}
	appRows, err := graph.Run(ctx, ssmCommitSeedAppsCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query ssm committed app seeds: %w", err)
	}
	platRows, err := graph.Run(ctx, ssmCommitSeedPlatsCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query ssm committed platform seeds: %w", err)
	}
	seen := map[string]bool{}
	var seeded []string
	for _, row := range append(appRows, platRows...) {
		key := ssmSeedHostKey(row)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		seeded = append(seeded, key)
	}
	sort.Strings(seeded)
	if len(seeded) > prefix {
		seeded = seeded[:prefix]
	}
	return seeded, nil
}

func ssmSeedHostKey(row map[string]any) string {
	parts := []string{
		stringFromMap(row, "h_partition"),
		stringFromMap(row, "h_account_id"),
		stringFromMap(row, "h_region"),
		stringFromMap(row, "h_instance_id"),
	}
	if parts[0] == "" && parts[1] == "" && parts[2] == "" && parts[3] == "" {
		return ""
	}
	return strings.Join(parts, "\x00")
}

// readSSMCommittedNodes fetches the committed observation rows for exactly
// the page hosts: application nodes with paired platforms, then platform
// nodes (which merge by uid into same-host entries or open platform-only
// entries for app-less hosts).
func readSSMCommittedNodes(ctx context.Context, graph GraphQuery, filter ssmHostInventoryFilter, page []string) (map[string]*ssmCommittedHost, error) {
	committed := map[string]*ssmCommittedHost{}
	if graph == nil || len(page) == 0 {
		return committed, nil
	}
	hosts := make([]map[string]any, 0, len(page))
	for _, key := range page {
		parts := strings.Split(key, "\x00")
		if len(parts) != 4 {
			continue
		}
		hosts = append(hosts, map[string]any{
			"partition":   parts[0],
			"account_id":  parts[1],
			"region":      parts[2],
			"instance_id": parts[3],
		})
	}
	params := map[string]any{
		"all_scopes": filter.AllScopes,
		"scope_ids":  append([]string{}, append(filter.AllowedScopeIDs, filter.AllowedRepositoryIDs...)...),
		"hosts":      hosts,
	}
	appRows, err := graph.Run(ctx, ssmCommitNodesAppsCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query ssm committed app nodes: %w", err)
	}
	for _, row := range appRows {
		app, _ := row["app"].(map[string]any)
		if app == nil {
			continue
		}
		host := ssmCommittedHostFor(committed, app)
		// One row per (app, paired platform): deduplicate the OPTIONAL
		// MATCH fan-out by uid so a multi-paired app serves once. A uid
		// can only be empty on corrupt data; those rows serve as before.
		if uid, _ := app["uid"].(string); uid == "" || !host.appUIDs[uid] {
			host.appUIDs[uid] = true
			host.apps = append(host.apps, app)
		}
		if plat, _ := row["plat"].(map[string]any); plat != nil {
			uid, _ := plat["uid"].(string)
			host.platforms[uid] = plat
		}
		if scope, _ := app["scope_id"].(string); scope != "" {
			host.scopeID = scope
		}
		if generation, _ := app["generation_id"].(string); generation != "" {
			host.generation = generation
		}
	}
	platRows, err := graph.Run(ctx, ssmCommitNodesPlatsCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query ssm committed platform nodes: %w", err)
	}
	for _, row := range platRows {
		plat, _ := row["plat_only"].(map[string]any)
		if plat == nil {
			continue
		}
		host := ssmCommittedHostFor(committed, plat)
		uid, _ := plat["uid"].(string)
		host.platforms[uid] = plat
		if host.scopeID == "" {
			if scope, _ := plat["scope_id"].(string); scope != "" {
				host.scopeID = scope
			}
		}
		if host.generation == "" {
			if generation, _ := plat["generation_id"].(string); generation != "" {
				host.generation = generation
			}
		}
	}
	return committed, nil
}

func ssmCommittedHostFor(committed map[string]*ssmCommittedHost, node map[string]any) *ssmCommittedHost {
	host := cloud.SSMHostTuple{
		Partition:  stringFromMap(node, "partition"),
		AccountID:  stringFromMap(node, "account_id"),
		Region:     stringFromMap(node, "region"),
		InstanceID: stringFromMap(node, "instance_id"),
	}
	key := ssmhost.HostKey(host)
	entry, ok := committed[key]
	if !ok {
		entry = &ssmCommittedHost{host: host, appUIDs: map[string]bool{}, platforms: map[string]map[string]any{}}
		committed[key] = entry
	}
	return entry
}
