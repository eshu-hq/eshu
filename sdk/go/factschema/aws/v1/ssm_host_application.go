// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// SSMInventoryTypeApplication is the only SSM inventory source type the
// v1.0.0 aws_ssm_host_application kind admits (eshu-hq/eshu#9556, accepted
// record #9302). Custom inventory, file and network entries, registry
// contents, and arbitrary command output are excluded: any other source
// type fails collection validation in sdk/go/collector, never silently
// becomes an application observation.
const SSMInventoryTypeApplication = "AWS:Application"

// SSMHostApplication is the schema-version-1 typed payload for the
// "aws_ssm_host_application" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issues #9302 and #9556).
//
// One fact is one allowlisted AWS:Application inventory row for one EC2
// managed node in one dated capture: the host tuple (Partition, AccountID,
// Region, InstanceID, ResourceType), the source envelope (SourceType,
// SchemaVersion, CaptureTime), and the six allowlisted row fields (Name,
// Version, Release, Epoch, PackageId, Architecture) when present. It is an
// observation, not a package finding: it carries no vulnerability verdict,
// no supply-chain impact, no PURL, and no image digest, and it must never be
// read as one. URL, Summary, hostnames, and IP addresses are excluded even
// though the API returns them.
//
// A missing or invalid CaptureTime emits no application fact and cannot
// supersede the last complete snapshot; an undated zero-row response is
// unknown, never an empty host. Identical rows in one capture deduplicate;
// another capture is a new observation.
type SSMHostApplication struct {
	// Partition is the AWS partition the managed node was observed in.
	// Required.
	Partition string `json:"partition"`

	// AccountID is the AWS account the managed node was observed in.
	// Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the managed node was observed in. Required.
	Region string `json:"region"`

	// InstanceID is the EC2 managed-node instance identifier. Required: an
	// identifier prefix alone is insufficient, and non-EC2 identities are
	// excluded from this kind.
	InstanceID string `json:"instance_id"`

	// ResourceType is the observed resource type, which must be the EC2
	// instance type for this kind. Required.
	ResourceType string `json:"resource_type"`

	// SourceType is the SSM inventory source type, which must be
	// SSMInventoryTypeApplication for this kind. Required.
	SourceType string `json:"source_type"`

	// SchemaVersion is the inventory schema version the row was read under.
	// Required.
	SchemaVersion string `json:"schema_version"`

	// CaptureTime is the SSM inventory capture time (RFC 3339), the source
	// freshness of this observation — never the agent ping time. Required:
	// a missing or invalid capture time emits no fact.
	CaptureTime string `json:"capture_time"`

	// Name is the allowlisted application Name field, when present.
	Name *string `json:"name,omitempty"`

	// Version is the allowlisted application Version field, when present.
	Version *string `json:"version,omitempty"`

	// Release is the allowlisted application Release field, when present.
	Release *string `json:"release,omitempty"`

	// Epoch is the allowlisted application Epoch field, when present.
	Epoch *string `json:"epoch,omitempty"`

	// PackageID is the allowlisted provider-specific package identifier,
	// when present. It is an inventory label, not a PURL and not proof of
	// package-manager, distro, or advisory semantics.
	PackageID *string `json:"package_id,omitempty"`

	// Architecture is the allowlisted application Architecture field, when
	// present.
	Architecture *string `json:"architecture,omitempty"`

	// CollectorInstanceID identifies the collector instance run that emitted
	// this fact. Optional: it is operational metadata about the run, not part
	// of the observation's identity.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// ssmHostApplicationCanonicalRow is the fixed-order canonical encoding of
// exactly the six allowlisted row fields. Nil decodes to "" so absent and
// present-but-empty hash identically: identity is about observed values,
// while the payload itself still records which fields were present.
type ssmHostApplicationCanonicalRow struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Release      string `json:"release"`
	Epoch        string `json:"epoch"`
	PackageID    string `json:"package_id"`
	Architecture string `json:"architecture"`
}

// canonicalSSMHostApplicationRow renders the allowlisted row fields in fixed
// order. Non-allowlisted content (collector run metadata, capture time, host
// tuple) can never affect the bytes, so it can never affect identity.
func canonicalSSMHostApplicationRow(app SSMHostApplication) []byte {
	row := ssmHostApplicationCanonicalRow{
		Name:         derefSSMString(app.Name),
		Version:      derefSSMString(app.Version),
		Release:      derefSSMString(app.Release),
		Epoch:        derefSSMString(app.Epoch),
		PackageID:    derefSSMString(app.PackageID),
		Architecture: derefSSMString(app.Architecture),
	}
	raw, err := json.Marshal(row)
	if err != nil {
		// A six-string struct always marshals; a failure here is a
		// programming error, not a runtime condition.
		panic(fmt.Sprintf("factschema: marshal canonical SSM application row: %v", err))
	}
	return raw
}

func derefSSMString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// CanonicalSSMHostApplicationRow returns the hex SHA-256 of the canonical
// allowlisted row: the row-identity third of the stable key. Hashing the
// canonical selected row is for identity, not a claim that the row is an OS
// package or that its contents are secret.
func CanonicalSSMHostApplicationRow(app SSMHostApplication) string {
	sum := sha256.Sum256(canonicalSSMHostApplicationRow(app))
	return fmt.Sprintf("%x", sum[:])
}

// SSMHostApplicationStableKey returns the stable key for one application
// observation: host tuple plus capture time plus the canonical row hash.
// Identical rows in a capture deduplicate on this key; another capture is a
// new observation because the capture time differs.
func SSMHostApplicationStableKey(app SSMHostApplication) string {
	return strings.Join([]string{
		"aws_ssm_host_application",
		app.Partition,
		app.AccountID,
		app.Region,
		app.InstanceID,
		app.CaptureTime,
		CanonicalSSMHostApplicationRow(app),
	}, "/")
}
