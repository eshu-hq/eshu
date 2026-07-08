// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

const (
	// FactKindReducerSupplyChainImpactFinding identifies one reducer-owned
	// supply-chain impact finding.
	FactKindReducerSupplyChainImpactFinding = "reducer_supply_chain_impact_finding"
	// FactKindReducerAWSCloudRuntimeDriftFinding identifies one AWS runtime
	// drift finding written by the reducer.
	FactKindReducerAWSCloudRuntimeDriftFinding = "reducer_aws_cloud_runtime_drift_finding"
	// FactKindReducerMultiCloudRuntimeDriftFinding identifies one provider-neutral
	// runtime drift finding written by the reducer.
	FactKindReducerMultiCloudRuntimeDriftFinding = "reducer_multi_cloud_runtime_drift_finding"
)

// DecodeReducerSupplyChainImpactFinding decodes env.Payload into the latest
// reducerderivedv1.SupplyChainImpactFinding struct.
func DecodeReducerSupplyChainImpactFinding(env Envelope) (reducerderivedv1.SupplyChainImpactFinding, error) {
	return decodeLatestMajor[reducerderivedv1.SupplyChainImpactFinding](FactKindReducerSupplyChainImpactFinding, env)
}

// EncodeReducerSupplyChainImpactFinding marshals a typed supply-chain impact
// finding into the map payload shape an Envelope carries.
func EncodeReducerSupplyChainImpactFinding(finding reducerderivedv1.SupplyChainImpactFinding) (map[string]any, error) {
	return encodeDirectPayload(finding)
}

// DecodeReducerAWSCloudRuntimeDriftFinding decodes env.Payload into the latest
// reducerderivedv1.AWSCloudRuntimeDriftFinding struct.
func DecodeReducerAWSCloudRuntimeDriftFinding(env Envelope) (reducerderivedv1.AWSCloudRuntimeDriftFinding, error) {
	return decodeLatestMajor[reducerderivedv1.AWSCloudRuntimeDriftFinding](FactKindReducerAWSCloudRuntimeDriftFinding, env)
}

// EncodeReducerAWSCloudRuntimeDriftFinding marshals a typed AWS runtime drift
// finding into the map payload shape an Envelope carries.
func EncodeReducerAWSCloudRuntimeDriftFinding(finding reducerderivedv1.AWSCloudRuntimeDriftFinding) (map[string]any, error) {
	return encodeDirectPayload(finding)
}

// DecodeReducerMultiCloudRuntimeDriftFinding decodes env.Payload into the
// latest reducerderivedv1.MultiCloudRuntimeDriftFinding struct.
func DecodeReducerMultiCloudRuntimeDriftFinding(env Envelope) (reducerderivedv1.MultiCloudRuntimeDriftFinding, error) {
	return decodeLatestMajor[reducerderivedv1.MultiCloudRuntimeDriftFinding](FactKindReducerMultiCloudRuntimeDriftFinding, env)
}

// EncodeReducerMultiCloudRuntimeDriftFinding marshals a typed provider-neutral
// runtime drift finding into the map payload shape an Envelope carries.
func EncodeReducerMultiCloudRuntimeDriftFinding(finding reducerderivedv1.MultiCloudRuntimeDriftFinding) (map[string]any, error) {
	return encodeDirectPayload(finding)
}
