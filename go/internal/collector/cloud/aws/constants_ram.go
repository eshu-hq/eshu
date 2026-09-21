// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceRAM identifies the regional AWS Resource Access Manager metadata
	// scan slice for one claimed account and region.
	ServiceRAM = "ram"
)

const (
	// ResourceTypeRAMResourceShare identifies an AWS Resource Access Manager
	// resource share metadata resource. Permission policy document bodies are
	// never part of this resource.
	ResourceTypeRAMResourceShare = "aws_ram_resource_share"
	// ResourceTypeRAMPermission identifies an AWS Resource Access Manager
	// managed-permission metadata summary (name, ARN, version, type). The
	// permission policy document body is never part of this resource.
	ResourceTypeRAMPermission = "aws_ram_permission"
)

const (
	// RelationshipRAMShareIncludesResource records that a resource share shares
	// one resource, targeting the shared resource's ARN with its RAM-reported
	// resource type as the relationship target type.
	RelationshipRAMShareIncludesResource = "ram_share_includes_resource"
	// RelationshipRAMShareTargetsAccount records that a resource share targets
	// one principal AWS account, targeting the bare account id that the
	// organizations scanner emits as an account resource id.
	RelationshipRAMShareTargetsAccount = "ram_share_targets_account"
	// RelationshipRAMShareTargetsOrganizationalUnit records that a resource
	// share targets one Organizations organizational unit principal, targeting
	// the OU ARN.
	RelationshipRAMShareTargetsOrganizationalUnit = "ram_share_targets_organizational_unit"
	// RelationshipRAMShareTargetsOrganization records that a resource share
	// targets an entire Organizations organization or root principal, targeting
	// the organization or root ARN.
	RelationshipRAMShareTargetsOrganization = "ram_share_targets_organization"
	// RelationshipRAMShareTargetsPrincipal records that a resource share targets
	// a principal whose form RAM did not report as a bare account id, an
	// Organizations OU ARN, or an organization or root ARN (for example a
	// service principal or a future RAM principal form). It uses the generic
	// resource target type and the raw principal id as the join key so the edge
	// stays honest without masquerading as an Organizations account.
	RelationshipRAMShareTargetsPrincipal = "ram_share_targets_principal"
	// RelationshipRAMShareUsesPermission records that a resource share uses one
	// managed permission, targeting the permission ARN.
	RelationshipRAMShareUsesPermission = "ram_share_uses_permission"
)
