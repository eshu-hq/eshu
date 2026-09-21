// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalogappregistry

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Service Catalog AppRegistry application
// and attribute-group observations for one AWS claim. Implementations read
// control-plane metadata through the AppRegistry list APIs and never read or
// persist attribute-group content bodies or associated-resource tag values.
type Client interface {
	// Snapshot returns every AppRegistry application visible to the configured
	// AWS credentials, each carrying its associated attribute groups and
	// associated resources, plus the standalone attribute groups in the account.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures AppRegistry application and attribute-group metadata plus
// non-fatal scan warnings.
type Snapshot struct {
	// Applications is the metadata-only set of AppRegistry applications, each
	// carrying its associated attribute groups and associated resources.
	Applications []Application
	// AttributeGroups is the metadata-only set of AppRegistry attribute groups
	// visible to the account, emitted as resource nodes in their own right.
	AttributeGroups []AttributeGroup
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Application is the scanner-owned AppRegistry application model. It carries
// control-plane metadata only.
type Application struct {
	// ID is the AppRegistry application identifier.
	ID string
	// ARN is the Amazon Resource Name that uniquely identifies the application.
	ARN string
	// Name is the application name (unique within the region).
	Name string
	// Description is the application description.
	Description string
	// CreationTime is when the application was created.
	CreationTime time.Time
	// LastUpdateTime is when the application was last updated.
	LastUpdateTime time.Time
	// Tags carries the application resource tags.
	Tags map[string]string
	// AttributeGroupARNs are the ARNs of the attribute groups associated with
	// this application, used to emit application-to-attribute-group edges.
	AttributeGroupARNs []string
	// AssociatedResources are the resources associated with this application
	// (for example CloudFormation stacks). Tag values and content bodies are
	// intentionally excluded.
	AssociatedResources []AssociatedResource
}

// AttributeGroup is the scanner-owned AppRegistry attribute group model. It
// carries control-plane metadata only and intentionally excludes the
// attribute-group content body (the application-metadata JSON document).
type AttributeGroup struct {
	// ID is the AppRegistry attribute group identifier.
	ID string
	// ARN is the Amazon Resource Name that uniquely identifies the group.
	ARN string
	// Name is the attribute group name.
	Name string
	// Description is the attribute group description.
	Description string
	// CreationTime is when the attribute group was created.
	CreationTime time.Time
	// LastUpdateTime is when the attribute group was last updated.
	LastUpdateTime time.Time
	// Tags carries the attribute group resource tags.
	Tags map[string]string
}

// AssociatedResource is the scanner-owned model for a resource associated with
// an AppRegistry application. It carries the resource identity and AppRegistry
// resource type only; the associated-resource tag value and any content detail
// are intentionally excluded so the scanner stays metadata-only.
type AssociatedResource struct {
	// ARN is the Amazon Resource Name of the associated resource. For CFN_STACK
	// associations this is the CloudFormation stack ARN.
	ARN string
	// Name is the associated resource name, when AWS reports one.
	Name string
	// ResourceType is the AppRegistry associated-resource type (for example
	// CFN_STACK or RESOURCE_TAG_VALUE).
	ResourceType string
}
