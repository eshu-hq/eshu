// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// neo4jECSBindingInstanceLookup lists materialized WorkloadInstance
// environments by workload name so the workload↔ECS-service binder can
// take its single-instance environment leg without inventing cross-env
// truth. One index-anchored read per intent, keyed by workload name
// like the sibling lookup.
type neo4jECSBindingInstanceLookup struct {
	reader query.GraphQuery
}

func (l neo4jECSBindingInstanceLookup) ListECSBindingInstanceEnvironments(
	ctx context.Context,
	workloadNames []string,
) (map[string][]string, error) {
	out := make(map[string][]string, len(workloadNames))
	if l.reader == nil || len(workloadNames) == 0 {
		return out, nil
	}
	rows, err := l.reader.Run(ctx, `
		MATCH (w:Workload)<-[:INSTANCE_OF]-(i:WorkloadInstance)
		WHERE w.name IN $names
		RETURN w.name AS name, i.environment AS environment
	`, map[string]any{"names": workloadNames})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]map[string]struct{}, len(workloadNames))
	for _, row := range rows {
		name := query.StringVal(row, "name")
		env := strings.TrimSpace(query.StringVal(row, "environment"))
		if name == "" || env == "" {
			continue
		}
		set, ok := seen[name]
		if !ok {
			set = make(map[string]struct{})
			seen[name] = set
		}
		set[env] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		envs := make([]string, 0, len(seen[name]))
		for env := range seen[name] {
			envs = append(envs, env)
		}
		sort.Strings(envs)
		out[name] = envs
	}
	return out, nil
}

var _ reducer.ECSBindingInstanceLookup = neo4jECSBindingInstanceLookup{}
