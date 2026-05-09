package postgres

import (
	"context"
	"fmt"
)

const activeReducerGraphWorkQuery = `
SELECT EXISTS (
    SELECT 1
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND domain IN (
        'deployment_mapping',
        'workload_materialization',
        'semantic_entity_materialization',
        'sql_relationship_materialization',
        'inheritance_materialization'
      )
)
`

// ReducerGraphDrain checks whether reducer graph-writing domains are still
// active before standalone shared-projection lanes write to the same backend.
type ReducerGraphDrain struct {
	queryer Queryer
}

// NewReducerGraphDrain constructs a reducer graph-drain checker.
func NewReducerGraphDrain(queryer Queryer) ReducerGraphDrain {
	return ReducerGraphDrain{queryer: queryer}
}

// HasActiveReducerGraphWork reports whether graph-writing reducer work is
// pending, retrying, claimed, or running.
func (d ReducerGraphDrain) HasActiveReducerGraphWork(ctx context.Context) (bool, error) {
	if d.queryer == nil {
		return false, fmt.Errorf("reducer graph drain queryer is required")
	}

	rows, err := d.queryer.QueryContext(ctx, activeReducerGraphWorkQuery)
	if err != nil {
		return false, fmt.Errorf("check active reducer graph work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, rows.Err()
	}

	var active bool
	if err := rows.Scan(&active); err != nil {
		return false, fmt.Errorf("scan active reducer graph work: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check active reducer graph work: %w", err)
	}

	return active, nil
}
