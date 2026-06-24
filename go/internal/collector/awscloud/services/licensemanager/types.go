// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package licensemanager

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS License Manager license-configuration
// observations for one AWS claim. Implementations read control-plane metadata
// through the License Manager management APIs and never grant, check out, or
// mutate license state, and never read entitlement tokens or usage records.
type Client interface {
	// Snapshot returns every License Manager license configuration visible to
	// the configured AWS credentials, each carrying the resource associations
	// reported for it.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures License Manager license-configuration metadata plus
// non-fatal scan warnings.
type Snapshot struct {
	// Configurations is the metadata-only set of License Manager license
	// configurations, each carrying its resource associations.
	Configurations []Configuration
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Configuration is the scanner-owned License Manager license-configuration
// model. It carries control-plane metadata only and intentionally excludes any
// entitlement token, checkout record, or license usage measurement.
type Configuration struct {
	// ARN is the Amazon Resource Name that uniquely identifies the license
	// configuration.
	ARN string
	// ID is the License Manager unique license-configuration id
	// (lic-xxxxxxxx...).
	ID string
	// Name is the license-configuration name.
	Name string
	// Description is the optional license-configuration description.
	Description string
	// Status is the reported license-configuration status (for example
	// AVAILABLE).
	Status string
	// LicenseCountingType is the dimension licenses are counted on (vCPU,
	// Instance, Core, or Socket).
	LicenseCountingType string
	// LicenseCount is the number of licenses managed by the configuration, when
	// reported.
	LicenseCount int64
	// LicenseCountConfigured reports whether AWS returned a managed license
	// count for the configuration (a zero count and an unset count are
	// distinguished without emitting an entitlement value).
	LicenseCountConfigured bool
	// LicenseCountHardLimit reports whether the available licenses are a hard
	// enforcement limit.
	LicenseCountHardLimit bool
	// ConsumedLicenses is the number of licenses consumed across associated
	// resources, when reported.
	ConsumedLicenses int64
	// LicenseRuleCount is the number of license rules attached to the
	// configuration. The rule expressions themselves are not persisted.
	LicenseRuleCount int
	// ProductInformationCount is the number of product-information filter sets
	// attached to the configuration. The filter expressions are not persisted.
	ProductInformationCount int
	// OwnerAccountID is the account id that owns the license configuration.
	OwnerAccountID string
	// LicenseExpiry is when the license configuration expires, when reported.
	LicenseExpiry time.Time
	// Tags carries the license-configuration resource tags.
	Tags map[string]string
	// Associations are the metadata-only resource associations reported for the
	// configuration.
	Associations []Association
}

// Association is the scanner-owned License Manager resource-association model.
// It records which AWS resource consumes licenses from a configuration, keyed
// by the resource ARN and its reported resource type. No entitlement or usage
// measurement is persisted.
type Association struct {
	// ResourceARN is the Amazon Resource Name of the associated resource that
	// consumes licenses from the configuration.
	ResourceARN string
	// ResourceType is the License-Manager-reported resource category for the
	// association (for example EC2_INSTANCE, EC2_HOST, EC2_AMI, RDS, or
	// SYSTEMS_MANAGER_MANAGED_INSTANCE).
	ResourceType string
	// ResourceOwnerID is the account id that owns the associated resource, when
	// reported.
	ResourceOwnerID string
	// AssociationTime is when the resource was associated with the
	// configuration, when reported.
	AssociationTime time.Time
}
