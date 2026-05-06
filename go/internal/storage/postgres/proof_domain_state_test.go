package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func (db *proofDomainDB) activeGenerationID(scopeID string) string {
	return db.state.activeGenerations[scopeID]
}

func (db *proofDomainDB) generationStatus(generationID string) scope.GenerationStatus {
	generation, ok := db.state.generations[generationID]
	if !ok {
		return ""
	}
	return generation.Status
}

func (db *proofDomainDB) projectorWorkItemCount() int {
	count := 0
	for _, item := range db.state.workItems {
		if item.stage == "projector" {
			count++
		}
	}
	return count
}

func (db *proofDomainDB) projectorWorkItemStatus(generationID string) string {
	for _, item := range db.state.workItems {
		if item.stage == "projector" && item.generationID == generationID {
			return item.status
		}
	}
	return ""
}

func (db *proofDomainDB) updateWorkItemStatus(stage string, scopeID string, generationID string, leaseOwner string, status string) (sql.Result, error) {
	for key, item := range db.state.workItems {
		if item.stage != stage || item.scopeID != scopeID || item.generationID != generationID || item.leaseOwner != leaseOwner {
			continue
		}

		item.status = status
		item.updatedAt = db.now
		item.leaseOwner = ""
		item.claimUntil = time.Time{}
		db.state.workItems[key] = item

		if stage == "projector" && status == "succeeded" {
			db.promoteGeneration(scopeID, generationID)
		}
		if stage == "projector" && status == "failed" {
			db.failGeneration(scopeID, generationID)
		}

		return proofResult{}, nil
	}

	return nil, fmt.Errorf("work item not found for stage=%s scope=%s generation=%s", stage, scopeID, generationID)
}

func (db *proofDomainDB) updateWorkItemStatusByID(workItemID string, leaseOwner string, status string) (sql.Result, error) {
	item, ok := db.state.workItems[workItemID]
	if !ok {
		return nil, fmt.Errorf("work item %q not found", workItemID)
	}
	if item.leaseOwner != leaseOwner {
		return nil, fmt.Errorf("work item %q lease owner = %q, want %q", workItemID, item.leaseOwner, leaseOwner)
	}

	item.status = status
	item.updatedAt = db.now
	item.leaseOwner = ""
	item.claimUntil = time.Time{}
	db.state.workItems[workItemID] = item
	if status == "succeeded" {
		db.reducerAcked++
	}

	return proofResult{}, nil
}

func (db *proofDomainDB) retryProjectorWork(scopeID string, generationID string, leaseOwner string, visibleAt time.Time) (sql.Result, error) {
	for key, item := range db.state.workItems {
		if item.stage != "projector" || item.scopeID != scopeID || item.generationID != generationID {
			continue
		}
		if item.leaseOwner != leaseOwner {
			return nil, fmt.Errorf("projector work item lease owner = %q, want %q", item.leaseOwner, leaseOwner)
		}

		item.status = "retrying"
		item.leaseOwner = ""
		item.claimUntil = time.Time{}
		item.visibleAt = visibleAt.UTC()
		item.updatedAt = db.now
		db.state.workItems[key] = item
		return proofResult{}, nil
	}

	return nil, fmt.Errorf("projector work item not found for scope=%s generation=%s", scopeID, generationID)
}

func (db *proofDomainDB) retryReducerWork(workItemID string, leaseOwner string, visibleAt time.Time) (sql.Result, error) {
	item, ok := db.state.workItems[workItemID]
	if !ok {
		return nil, fmt.Errorf("reducer work item %q not found", workItemID)
	}
	if item.leaseOwner != leaseOwner {
		return nil, fmt.Errorf("reducer work item lease owner = %q, want %q", item.leaseOwner, leaseOwner)
	}

	item.status = "retrying"
	item.leaseOwner = ""
	item.claimUntil = time.Time{}
	item.visibleAt = visibleAt.UTC()
	item.updatedAt = db.now
	db.state.workItems[workItemID] = item
	return proofResult{}, nil
}

func (db *proofDomainDB) promoteGeneration(scopeID string, generationID string) {
	for key, generation := range db.state.generations {
		if generation.ScopeID != scopeID {
			continue
		}
		if key == generationID {
			generation.Status = scope.GenerationStatusActive
			db.state.generations[key] = generation
			continue
		}
		if generation.Status == scope.GenerationStatusActive {
			generation.Status = scope.GenerationStatusSuperseded
			db.state.generations[key] = generation
		}
	}

	db.state.activeGenerations[scopeID] = generationID
	db.state.scopeStatuses[scopeID] = string(scope.GenerationStatusActive)
}

func (db *proofDomainDB) failGeneration(scopeID string, generationID string) {
	generation, ok := db.state.generations[generationID]
	if ok {
		generation.Status = scope.GenerationStatusFailed
		db.state.generations[generationID] = generation
	}
	if db.state.activeGenerations[scopeID] == generationID {
		delete(db.state.activeGenerations, scopeID)
		db.state.scopeStatuses[scopeID] = string(scope.GenerationStatusFailed)
	}
}
