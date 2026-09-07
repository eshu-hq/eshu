// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// Family-fixture aliases: the per-family Odù fixtures moved to
// go/internal/ifa/familyodu when this package reached its directory file cap
// (#6594 P1). The aliases below keep every previously exported ifa name
// resolving so external callers (cmd/ifa, throughput, materializededges,
// coverage manifests) observe no API break across the move.

type (
	// Odu is the family-fixture scenario envelope (familyodu.Odu).
	Odu = familyodu.Odu
	// CatalogOdu pairs a cataloged Odu with its human-readable detail line.
	CatalogOdu = familyodu.CatalogOdu
)

const (
	CodeownersFamilyOduName                     = familyodu.CodeownersFamilyOduName
	CodeCallFamilyOduName                       = familyodu.CodeCallFamilyOduName
	ContentEntityFactKind                       = familyodu.ContentEntityFactKind
	ContentFactKind                             = familyodu.ContentFactKind
	DeployableUnitFamilyOduName                 = familyodu.DeployableUnitFamilyOduName
	DocumentationFamilyCassettePath             = familyodu.DocumentationFamilyCassettePath
	DocumentationFamilyOduName                  = familyodu.DocumentationFamilyOduName
	FileFactKind                                = familyodu.FileFactKind
	IAMCanAssumeFamilyOduName                   = familyodu.IAMCanAssumeFamilyOduName
	IAMInstanceProfileRoleFamilyOduName         = familyodu.IAMInstanceProfileRoleFamilyOduName
	InheritanceFamilyOduName                    = familyodu.InheritanceFamilyOduName
	KubernetesNamespaceEnvironmentFamilyOduName = familyodu.KubernetesNamespaceEnvironmentFamilyOduName
	RationaleFamilyOduName                      = familyodu.RationaleFamilyOduName
	RepoDependencyFamilyOduName                 = familyodu.RepoDependencyFamilyOduName
	RepositoryFactKind                          = familyodu.RepositoryFactKind
	SharedFollowupFactKind                      = familyodu.SharedFollowupFactKind
	ShellExecFamilyOduName                      = familyodu.ShellExecFamilyOduName
	SubmodulePinFamilyOduName                   = familyodu.SubmodulePinFamilyOduName
	SymbolRuntimeFamilyOduName                  = familyodu.SymbolRuntimeFamilyOduName
	WorkloadCloudRelationshipFamilyOduName      = familyodu.WorkloadCloudRelationshipFamilyOduName
	WorkloadDependencyFamilyOduName             = familyodu.WorkloadDependencyFamilyOduName
)

// CodeCallFamilyCassetteFullPath resolves the code-call cassette path under repoRoot.
func CodeCallFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.CodeCallFamilyCassetteFullPath(repoRoot)
}

// CodeownersFamilyCassetteFullPath resolves the codeowners cassette path under repoRoot.
func CodeownersFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.CodeownersFamilyCassetteFullPath(repoRoot)
}

// DeployableUnitFamilyCassetteFullPath resolves the deployable-unit cassette path under repoRoot.
func DeployableUnitFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.DeployableUnitFamilyCassetteFullPath(repoRoot)
}

// DocumentationFamilyCassetteFullPath resolves the documentation cassette path under repoRoot.
func DocumentationFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.DocumentationFamilyCassetteFullPath(repoRoot)
}

// HandlesRouteFamilyExpectedEdgesPath resolves the handles-route expected-edges path under repoRoot.
func HandlesRouteFamilyExpectedEdgesPath(repoRoot string) string {
	return familyodu.HandlesRouteFamilyExpectedEdgesPath(repoRoot)
}

// InvokesCloudActionFamilyExpectedEdgesPath resolves the invokes-cloud-action expected-edges path under repoRoot.
func InvokesCloudActionFamilyExpectedEdgesPath(repoRoot string) string {
	return familyodu.InvokesCloudActionFamilyExpectedEdgesPath(repoRoot)
}

// LoadCassetteEnvelopes decodes the cassette at path into fact envelopes.
func LoadCassetteEnvelopes(path string) ([]facts.Envelope, error) {
	return familyodu.LoadCassetteEnvelopes(path) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// RationaleFamilyFileFact builds the rationale family file fact for file.
func RationaleFamilyFileFact(file codegraphv1.File) facts.Envelope {
	return familyodu.RationaleFamilyFileFact(file)
}

// ShellExecFamilyRepositoryFact builds the shell-exec family repository fact for repository.
func ShellExecFamilyRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	return familyodu.ShellExecFamilyRepositoryFact(repository)
}

// ShellExecFamilyFileFact builds the shell-exec family file fact for file.
func ShellExecFamilyFileFact(file codegraphv1.File) facts.Envelope {
	return familyodu.ShellExecFamilyFileFact(file)
}

// RationaleFamilyRepositoryFact builds the rationale family repository fact for repository.
func RationaleFamilyRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	return familyodu.RationaleFamilyRepositoryFact(repository)
}

// RepoDependencyFamilyCassetteFullPath resolves the repo-dependency cassette path under repoRoot.
func RepoDependencyFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.RepoDependencyFamilyCassetteFullPath(repoRoot)
}

// RepoDependencyFamilySourceCoordinates extracts the source coordinates from odu.
func RepoDependencyFamilySourceCoordinates(odu Odu) (string, string, error) {
	return familyodu.RepoDependencyFamilySourceCoordinates(odu) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// RunsInFamilyExpectedEdgesPath resolves the runs-in expected-edges path under repoRoot.
func RunsInFamilyExpectedEdgesPath(repoRoot string) string {
	return familyodu.RunsInFamilyExpectedEdgesPath(repoRoot)
}

// ShellExecFamilyCassetteFullPath resolves the shell-exec cassette path under repoRoot.
func ShellExecFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.ShellExecFamilyCassetteFullPath(repoRoot)
}

// ShellExecFamilyExpectedEdgesPath resolves the shell-exec expected-edges path under repoRoot.
func ShellExecFamilyExpectedEdgesPath(repoRoot string) string {
	return familyodu.ShellExecFamilyExpectedEdgesPath(repoRoot)
}

// SubmodulePinFamilyCassetteFullPath resolves the submodule-pin cassette path under repoRoot.
func SubmodulePinFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.SubmodulePinFamilyCassetteFullPath(repoRoot)
}

// SymbolRuntimeFamilyCassetteFullPath resolves the symbol-runtime cassette path under repoRoot.
func SymbolRuntimeFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.SymbolRuntimeFamilyCassetteFullPath(repoRoot)
}

// WorkloadDependencyFamilyCassetteFullPath resolves the workload-dependency cassette path under repoRoot.
func WorkloadDependencyFamilyCassetteFullPath(repoRoot string) string {
	return familyodu.WorkloadDependencyFamilyCassetteFullPath(repoRoot)
}

// IAMCanAssumeFamilyOdu returns the cataloged IAM can-assume family Odù.
func IAMCanAssumeFamilyOdu() CatalogOdu { return familyodu.IAMCanAssumeFamilyOdu() }

// IAMInstanceProfileRoleFamilyOdu returns the cataloged instance-profile-role family Odù.
func IAMInstanceProfileRoleFamilyOdu() CatalogOdu {
	return familyodu.IAMInstanceProfileRoleFamilyOdu()
}

// InheritanceFamilyOdu returns the cataloged inheritance family Odù.
func InheritanceFamilyOdu() CatalogOdu { return familyodu.InheritanceFamilyOdu() }

// KubernetesNamespaceEnvironmentFamilyOdu returns the cataloged namespace-environment family Odù.
func KubernetesNamespaceEnvironmentFamilyOdu() CatalogOdu {
	return familyodu.KubernetesNamespaceEnvironmentFamilyOdu()
}

// RationaleFamilyOdu returns the cataloged rationale family Odù.
func RationaleFamilyOdu() CatalogOdu { return familyodu.RationaleFamilyOdu() }

// ShellExecFamilyOdu returns the cataloged shell-exec family Odù.
func ShellExecFamilyOdu() CatalogOdu { return familyodu.ShellExecFamilyOdu() }

// SymbolRuntimeFamilyOdu returns the cataloged symbol-runtime family Odù.
func SymbolRuntimeFamilyOdu() CatalogOdu { return familyodu.SymbolRuntimeFamilyOdu() }

// WorkloadCloudRelationshipFamilyOdu returns the cataloged workload-cloud-relationship family Odù.
func WorkloadCloudRelationshipFamilyOdu() CatalogOdu {
	return familyodu.WorkloadCloudRelationshipFamilyOdu()
}

// LoadCodeCallFamilyOdu loads the code-call family Odù from cassettePath.
func LoadCodeCallFamilyOdu(cassettePath string) (Odu, error) {
	return familyodu.LoadCodeCallFamilyOdu(cassettePath) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// LoadCodeownersFamilyOdu loads the codeowners family Odù from cassettePath.
func LoadCodeownersFamilyOdu(cassettePath string) (Odu, error) {
	return familyodu.LoadCodeownersFamilyOdu(cassettePath) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// LoadDeployableUnitFamilyOdu loads the deployable-unit family Odù from cassettePath.
func LoadDeployableUnitFamilyOdu(cassettePath string) (Odu, error) {
	return familyodu.LoadDeployableUnitFamilyOdu(cassettePath) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// LoadDocumentationFamilyOdu loads the documentation family Odù from cassettePath.
func LoadDocumentationFamilyOdu(cassettePath string) (Odu, error) {
	return familyodu.LoadDocumentationFamilyOdu(cassettePath) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// LoadRepoDependencyFamilyOdu loads the repo-dependency family Odù from cassettePath.
func LoadRepoDependencyFamilyOdu(cassettePath string) (Odu, error) {
	return familyodu.LoadRepoDependencyFamilyOdu(cassettePath) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// LoadSubmodulePinFamilyOdu loads the submodule-pin family Odù from cassettePath.
func LoadSubmodulePinFamilyOdu(cassettePath string) (Odu, error) {
	return familyodu.LoadSubmodulePinFamilyOdu(cassettePath) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// LoadWorkloadDependencyFamilyOdu loads the workload-dependency family Odù from cassettePath.
func LoadWorkloadDependencyFamilyOdu(cassettePath string) (Odu, error) {
	return familyodu.LoadWorkloadDependencyFamilyOdu(cassettePath) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

const (
	CodeCallFamilyCassettePath             = familyodu.CodeCallFamilyCassettePath
	InheritanceFamilyCassetteRelPath       = familyodu.InheritanceFamilyCassetteRelPath
	InheritanceFamilyRepoID                = familyodu.InheritanceFamilyRepoID
	RationaleFamilyCassetteRelPath         = familyodu.RationaleFamilyCassetteRelPath
	RationaleFamilyChargeLine              = familyodu.RationaleFamilyChargeLine
	RationaleFamilyChargeName              = familyodu.RationaleFamilyChargeName
	RationaleFamilyChargePath              = familyodu.RationaleFamilyChargePath
	RationaleFamilyDeltaCassetteRelPath    = familyodu.RationaleFamilyDeltaCassetteRelPath
	RationaleFamilyDeltaGenerationID       = familyodu.RationaleFamilyDeltaGenerationID
	RationaleFamilyGenerationID            = familyodu.RationaleFamilyGenerationID
	RationaleFamilyHealthcheckPath         = familyodu.RationaleFamilyHealthcheckPath
	RationaleFamilyInvoiceLine             = familyodu.RationaleFamilyInvoiceLine
	RationaleFamilyInvoiceName             = familyodu.RationaleFamilyInvoiceName
	RationaleFamilyInvoicePath             = familyodu.RationaleFamilyInvoicePath
	RationaleFamilyLocalPath               = familyodu.RationaleFamilyLocalPath
	RationaleFamilyReconcilePath           = familyodu.RationaleFamilyReconcilePath
	RationaleFamilyRefundPath              = familyodu.RationaleFamilyRefundPath
	RationaleFamilyRepoID                  = familyodu.RationaleFamilyRepoID
	RationaleFamilyScopeID                 = familyodu.RationaleFamilyScopeID
	RationaleFamilySourceRunID             = familyodu.RationaleFamilySourceRunID
	ShellExecFamilyCleanupFunctionLine     = familyodu.ShellExecFamilyCleanupFunctionLine
	ShellExecFamilyCleanupFunctionName     = familyodu.ShellExecFamilyCleanupFunctionName
	ShellExecFamilyCleanupFunctionUID      = familyodu.ShellExecFamilyCleanupFunctionUID
	ShellExecFamilyCleanupPath             = familyodu.ShellExecFamilyCleanupPath
	ShellExecFamilyDeployFunctionLine      = familyodu.ShellExecFamilyDeployFunctionLine
	ShellExecFamilyDeployFunctionName      = familyodu.ShellExecFamilyDeployFunctionName
	ShellExecFamilyDeployFunctionUID       = familyodu.ShellExecFamilyDeployFunctionUID
	ShellExecFamilyDeployPath              = familyodu.ShellExecFamilyDeployPath
	ShellExecFamilyDeployTarget1           = familyodu.ShellExecFamilyDeployTarget1
	ShellExecFamilyDeployTarget2           = familyodu.ShellExecFamilyDeployTarget2
	ShellExecFamilyLocalPath               = familyodu.ShellExecFamilyLocalPath
	ShellExecFamilyOrphanFunctionLine      = familyodu.ShellExecFamilyOrphanFunctionLine
	ShellExecFamilyOrphanFunctionName      = familyodu.ShellExecFamilyOrphanFunctionName
	ShellExecFamilyOrphanFunctionUID       = familyodu.ShellExecFamilyOrphanFunctionUID
	ShellExecFamilyOrphanPath              = familyodu.ShellExecFamilyOrphanPath
	ShellExecFamilyRepoID                  = familyodu.ShellExecFamilyRepoID
	ShellExecFamilySilentFunctionLine      = familyodu.ShellExecFamilySilentFunctionLine
	ShellExecFamilySilentFunctionName      = familyodu.ShellExecFamilySilentFunctionName
	ShellExecFamilySilentFunctionUID       = familyodu.ShellExecFamilySilentFunctionUID
	ShellExecFamilySilentPath              = familyodu.ShellExecFamilySilentPath
	SymbolRuntimeFamilyActionID            = familyodu.SymbolRuntimeFamilyActionID
	SymbolRuntimeFamilyCallerFunctionEnd   = familyodu.SymbolRuntimeFamilyCallerFunctionEnd
	SymbolRuntimeFamilyCallerFunctionLine  = familyodu.SymbolRuntimeFamilyCallerFunctionLine
	SymbolRuntimeFamilyCallerFunctionName  = familyodu.SymbolRuntimeFamilyCallerFunctionName
	SymbolRuntimeFamilyCallerFunctionUID   = familyodu.SymbolRuntimeFamilyCallerFunctionUID
	SymbolRuntimeFamilyCloudAction         = familyodu.SymbolRuntimeFamilyCloudAction
	SymbolRuntimeFamilyCloudActionCallLine = familyodu.SymbolRuntimeFamilyCloudActionCallLine
	SymbolRuntimeFamilyCloudActionMethod   = familyodu.SymbolRuntimeFamilyCloudActionMethod
	SymbolRuntimeFamilyCloudActionService  = familyodu.SymbolRuntimeFamilyCloudActionService
	SymbolRuntimeFamilyDockerfilePath      = familyodu.SymbolRuntimeFamilyDockerfilePath
	SymbolRuntimeFamilyEndpointID          = familyodu.SymbolRuntimeFamilyEndpointID
	SymbolRuntimeFamilyHandlerFunctionEnd  = familyodu.SymbolRuntimeFamilyHandlerFunctionEnd
	SymbolRuntimeFamilyHandlerFunctionLine = familyodu.SymbolRuntimeFamilyHandlerFunctionLine
	SymbolRuntimeFamilyHandlerFunctionName = familyodu.SymbolRuntimeFamilyHandlerFunctionName
	SymbolRuntimeFamilyHandlerFunctionUID  = familyodu.SymbolRuntimeFamilyHandlerFunctionUID
	SymbolRuntimeFamilyHealthEndpointID    = familyodu.SymbolRuntimeFamilyHealthEndpointID
	SymbolRuntimeFamilyHealthFunctionEnd   = familyodu.SymbolRuntimeFamilyHealthFunctionEnd
	SymbolRuntimeFamilyHealthFunctionLine  = familyodu.SymbolRuntimeFamilyHealthFunctionLine
	SymbolRuntimeFamilyHealthFunctionName  = familyodu.SymbolRuntimeFamilyHealthFunctionName
	SymbolRuntimeFamilyHealthFunctionUID   = familyodu.SymbolRuntimeFamilyHealthFunctionUID
	SymbolRuntimeFamilyHealthMethod        = familyodu.SymbolRuntimeFamilyHealthMethod
	SymbolRuntimeFamilyHealthRoutePath     = familyodu.SymbolRuntimeFamilyHealthRoutePath
	SymbolRuntimeFamilyJenkinsfilePath     = familyodu.SymbolRuntimeFamilyJenkinsfilePath
	SymbolRuntimeFamilyLocalPath           = familyodu.SymbolRuntimeFamilyLocalPath
	SymbolRuntimeFamilyNonCatalogCallLine  = familyodu.SymbolRuntimeFamilyNonCatalogCallLine
	SymbolRuntimeFamilyNonCatalogMethod    = familyodu.SymbolRuntimeFamilyNonCatalogMethod
	SymbolRuntimeFamilyNonCatalogService   = familyodu.SymbolRuntimeFamilyNonCatalogService
	SymbolRuntimeFamilyRepoID              = familyodu.SymbolRuntimeFamilyRepoID
	SymbolRuntimeFamilyRouteFramework      = familyodu.SymbolRuntimeFamilyRouteFramework
	SymbolRuntimeFamilyRouteMethod         = familyodu.SymbolRuntimeFamilyRouteMethod
	SymbolRuntimeFamilyRouteMethodPost     = familyodu.SymbolRuntimeFamilyRouteMethodPost
	SymbolRuntimeFamilyRoutePath           = familyodu.SymbolRuntimeFamilyRoutePath
	SymbolRuntimeFamilyServerPath          = familyodu.SymbolRuntimeFamilyServerPath
	SymbolRuntimeFamilyWorkloadID          = familyodu.SymbolRuntimeFamilyWorkloadID
)
