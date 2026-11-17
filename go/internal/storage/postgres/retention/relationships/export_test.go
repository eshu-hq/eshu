// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

// Stage names the fault hook receives, for external live tests.
const (
	StageSetLockTimeout    = stageSetLockTimeout
	StageTryLock           = stageTryLock
	StageClassify          = stageClassify
	StageDeleteEvidence    = stageDeleteEvidence
	StageDeleteCandidates  = stageDeleteCandidates
	StageDeleteGenerations = stageDeleteGenerations
)

// SetAfterStatement installs a hook that runs after each batch statement, so an
// external live test can park a batch between two statements or fail it.
func SetAfterStatement(opts *Options, hook func(stage string) error) { opts.afterStatement = hook }

// Statement text the live tests execute directly, to prove each delete guards
// itself against ids the classification would never have passed.
const (
	DeleteEvidenceSQL    = deleteEvidenceQuery
	DeleteCandidatesSQL  = deleteCandidatesQuery
	DeleteGenerationsSQL = deleteGenerationsQuery
	ClassifySQL          = classifyQuery
	CensusEvidenceSQL    = censusEvidenceQuery
	CensusCandidatesSQL  = censusCandidatesQuery
	CensusGenerationsSQL = censusGenerationsQuery
)
