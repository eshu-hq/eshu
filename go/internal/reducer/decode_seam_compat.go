// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the remaining root call sites keep their current spelling; each
// entry is deleted once its last caller has moved into a family subpackage.
// The four incident-routing entries were removed when their only callers
// moved into internal/reducer/incident, which imports schemadecode directly.

var (
	codegraphDecodeQuarantine              = schemadecode.CodegraphDecodeQuarantine
	decodeAWSIAMPermission                 = schemadecode.DecodeAWSIAMPermission
	decodeAWSImageReference                = schemadecode.DecodeAWSImageReference
	decodeAWSRelationship                  = schemadecode.DecodeAWSRelationship
	decodeAWSResource                      = schemadecode.DecodeAWSResource
	decodeAWSSecurityGroupRule             = schemadecode.DecodeAWSSecurityGroupRule
	decodeAttestationSLSAProvenance        = schemadecode.DecodeAttestationSLSAProvenance
	decodeAttestationSignatureVerification = schemadecode.DecodeAttestationSignatureVerification
	decodeAttestationStatement             = schemadecode.DecodeAttestationStatement
	decodeAzureCloudRelationship           = schemadecode.DecodeAzureCloudRelationship
	decodeAzureCloudResource               = schemadecode.DecodeAzureCloudResource
	decodeAzureImageReference              = schemadecode.DecodeAzureImageReference
	decodeCICDArtifact                     = schemadecode.DecodeCICDArtifact
	decodeCICDRun                          = schemadecode.DecodeCICDRun
	decodeCICDWorkflowImageEvidence        = schemadecode.DecodeCICDWorkflowImageEvidence
	decodeCodeFunctionSource               = schemadecode.DecodeCodeFunctionSource
	decodeCodeFunctionSummary              = schemadecode.DecodeCodeFunctionSummary
	decodeCodeInterprocEvidence            = schemadecode.DecodeCodeInterprocEvidence
	decodeCodeTaintEvidence                = schemadecode.DecodeCodeTaintEvidence
	decodeCodegraphFile                    = schemadecode.DecodeCodegraphFile
	decodeCodegraphRepository              = schemadecode.DecodeCodegraphRepository
	decodeCodeownersOwnership              = schemadecode.DecodeCodeownersOwnership
	decodeDocumentationDocument            = schemadecode.DecodeDocumentationDocument
	decodeDocumentationEntityMention       = schemadecode.DecodeDocumentationEntityMention
	decodeEC2InstancePosture               = schemadecode.DecodeEC2InstancePosture
	decodeGCPCloudRelationship             = schemadecode.DecodeGCPCloudRelationship
	decodeGCPCloudResource                 = schemadecode.DecodeGCPCloudResource
	decodeGCPImageReference                = schemadecode.DecodeGCPImageReference
	decodeKubernetesLiveNamespace          = schemadecode.DecodeKubernetesLiveNamespace
	decodeKubernetesLivePodTemplate        = schemadecode.DecodeKubernetesLivePodTemplate
	decodeKubernetesLiveRelationship       = schemadecode.DecodeKubernetesLiveRelationship
	decodeKubernetesLiveWarning            = schemadecode.DecodeKubernetesLiveWarning
	decodeOCIImageIndexForIndex            = schemadecode.DecodeOCIImageIndexForIndex
)
