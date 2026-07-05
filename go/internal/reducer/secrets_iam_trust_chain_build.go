// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// secretsIAMPrincipal pairs an aws_iam_principal envelope with its decoded typed
// payload so the trust-chain build decodes each principal exactly once, at index
// build time, and quarantines a malformed one there rather than re-decoding (and
// re-failing) per role in secretsIAMRoleCloudResourceUID.
type secretsIAMPrincipal struct {
	env     facts.Envelope
	decoded iamv1.Principal
}

// secretsIAMServiceAccount pairs a k8s_service_account envelope with its
// decoded typed payload (Wave 4d K8S lane), decoded once at index build time.
type secretsIAMServiceAccount struct {
	env     facts.Envelope
	decoded secretsiamv1.KubernetesServiceAccount
}

// secretsIAMWorkload pairs a k8s_workload_identity_use envelope with its
// decoded typed payload (Wave 4d K8S lane), decoded once at index build time.
type secretsIAMWorkload struct {
	env     facts.Envelope
	decoded secretsiamv1.KubernetesWorkloadIdentityUse
}

// secretsIAMIRSA pairs an eks_irsa_annotation or eks_pod_identity_association
// envelope with its decoded typed payload (Wave 4d K8S lane), decoded once at
// index build time. The two kinds share an index (index.irsa) because they are
// interchangeable identity-provider evidence for the same service account, so
// this struct carries both decoded shapes flattened to the fields the
// trust-chain build actually reads (ServiceAccountJoinKey, RoleARN, and the
// IRSA-only WebIdentitySubjectFingerprint).
type secretsIAMIRSA struct {
	env                           facts.Envelope
	roleARN                       string
	webIdentitySubjectFingerprint string
}

// secretsIAMVaultRole pairs a vault_auth_role envelope with its decoded typed
// payload (Wave 4d VAULT lane), decoded once at index build time.
type secretsIAMVaultRole struct {
	env     facts.Envelope
	decoded secretsiamv1.VaultAuthRole
}

// secretsIAMVaultPolicy pairs a vault_acl_policy envelope with its decoded
// typed payload (Wave 4d VAULT lane), decoded once at index build time.
type secretsIAMVaultPolicy struct {
	env     facts.Envelope
	decoded secretsiamv1.VaultACLPolicy
}

// secretsIAMVaultKV pairs a vault_kv_metadata envelope with its decoded typed
// payload (Wave 4d VAULT lane), decoded once at index build time.
type secretsIAMVaultKV struct {
	env     facts.Envelope
	decoded secretsiamv1.VaultKVMetadata
}

// secretsIAMGCPBinding pairs a k8s_gcp_workload_identity_binding envelope with
// its decoded typed payload (Wave 4d K8S lane), decoded once at index build
// time. Unlike gcpTrusts/gcpPrincipals/gcpPermissions (the deferred gcp_iam
// lane, read raw), this K8S-lane kind's OWN fields decode through the typed
// seam; only its downstream join against the deferred gcp_iam_trust_policy
// raw envelope stays on payloadString reads (see
// secretsIAMGCPExactChainsForServiceAccount).
type secretsIAMGCPBinding struct {
	env     facts.Envelope
	decoded secretsiamv1.KubernetesGCPWorkloadIdentityBinding
}

type secretsIAMIndex struct {
	// serviceAccounts, workloads, irsa, and vaultRoles hold only the K8S/VAULT
	// lane facts that decoded cleanly, keyed by service_account_join_key (or
	// role_join_key for vaultRoles' bound service accounts). A malformed fact
	// is quarantined during buildSecretsIAMIndex and never enters the index,
	// so the trust-chain build reads only valid, pre-decoded evidence.
	serviceAccounts map[string][]secretsIAMServiceAccount
	workloads       map[string][]secretsIAMWorkload
	irsa            map[string][]secretsIAMIRSA
	vaultRoles      map[string][]secretsIAMVaultRole
	vaultAuthRoles  []secretsIAMVaultRole
	// iamPrincipals holds only the aws_iam_principal facts that decoded cleanly,
	// keyed by principal_arn. A malformed principal is quarantined during
	// buildSecretsIAMIndex and never enters the index, so the trust-chain build
	// reads only valid principals.
	iamPrincipals map[string][]secretsIAMPrincipal
	iamTrusts     map[string][]facts.Envelope
	vaultPolicies map[string][]secretsIAMVaultPolicy
	vaultKV       map[string][]secretsIAMVaultKV
	// gcpPrincipals, gcpTrusts, and gcpPermissions stay on raw envelopes for
	// the gcp_iam_principal/gcp_iam_trust_policy/gcp_iam_permission_policy
	// kinds: deferred: gcp_iam lane, Wave 4d types vault/k8s only.
	gcpPrincipals map[string][]facts.Envelope
	gcpTrusts     map[string][]facts.Envelope
	// gcpK8sBindings holds the K8S-lane k8s_gcp_workload_identity_binding
	// kind decoded through the typed seam (secretsIAMGCPBinding): this kind
	// IS in scope for Wave 4d. Only its downstream join against the deferred
	// gcp_iam_trust_policy raw envelope stays on payloadString reads (see
	// secretsIAMGCPExactChainsForServiceAccount).
	gcpK8sBindings map[string][]secretsIAMGCPBinding
	gcpPermissions map[string][]facts.Envelope
	coverage       []facts.Envelope
}

// BuildSecretsIAMTrustChainReadModels builds reducer-owned secrets/IAM read
// models from redacted source facts. It is pure so exact, partial, stale, and
// unsupported behavior can be proven without Postgres, graph, or provider calls.
//
// A malformed source fact whose payload is missing a required identity field
// (an input_invalid decode failure) is quarantined per-fact and returned in the
// []quarantinedFact slice: it is skipped so the valid trust chains still
// project, matching the per-fact isolation contract every other migrated
// reducer kind follows. This applies to the AWS IAM lane (aws_iam_principal,
// already migrated in #4568), the VAULT lane (vault_auth_role,
// vault_acl_policy, vault_kv_metadata), and the K8S lane
// (k8s_service_account, k8s_workload_identity_use, eks_irsa_annotation,
// eks_pod_identity_association, k8s_gcp_workload_identity_binding) — Wave 4d,
// Contract System v1 #4566/#4582. The GCP IAM lane
// (gcp_iam_principal/gcp_iam_trust_policy/gcp_iam_permission_policy) is
// deferred and still reads raw payloadString/payloadBool.
//
// Any OTHER decode error (a non-input_invalid classification the seam may add
// later, or a non-*factDecodeError) is FATAL and returned as the error: it fails
// the whole work item so the durable queue triages it, rather than being
// swallowed as a silent success.
func BuildSecretsIAMTrustChainReadModels(envelopes []facts.Envelope) (SecretsIAMTrustChainReadModels, []quarantinedFact, error) {
	index, quarantined, err := buildSecretsIAMIndex(envelopes)
	if err != nil {
		return SecretsIAMTrustChainReadModels{}, nil, err
	}
	models := SecretsIAMTrustChainReadModels{}
	models.PostureGaps = append(models.PostureGaps, secretsIAMCoverageGaps(index.coverage)...)
	models.PostureGaps = append(models.PostureGaps, secretsIAMStaleGenerationGaps(index)...)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMWildcardTrustObservations(index.iamTrusts)...,
	)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMWildcardVaultAuthRoleObservations(index.vaultAuthRoles)...,
	)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMExternalTrustObservations(index.iamTrusts)...,
	)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMGCPGrantObservations(index)...,
	)
	chains, paths, gaps := secretsIAMExactChains(index)
	models.IdentityTrustChains = append(models.IdentityTrustChains, chains...)
	models.SecretAccessPaths = append(models.SecretAccessPaths, paths...)
	models.PostureGaps = append(models.PostureGaps, gaps...)
	sortSecretsIAMReadModels(&models)
	return models, quarantined, nil
}

// quarantineOrFatal routes a decode error through partitionDecodeFailures and
// either appends the resulting quarantinedFact to quarantined (returning
// ok=true to tell the caller to skip this envelope and continue the loop) or
// signals a fatal error the caller must return immediately. It centralizes the
// exact fatal-passthrough contract every decode call site in
// buildSecretsIAMIndex must follow: a decode error that is NOT classified
// input_invalid (a future schema-mismatch/unsupported-major class, or a
// non-*factDecodeError) is terminal and must fail the whole trust-chain work
// item so the durable queue triages it — swallowing it here would let a
// malformed fact Ack as success, the "swallow failures" hole the redesign
// exists to close.
func quarantineOrFatal(envelope facts.Envelope, err error, quarantined *[]quarantinedFact) (fatal error, skip bool) {
	q, ok, fatalErr := partitionDecodeFailures(envelope, err)
	if fatalErr != nil {
		return fatalErr, false
	}
	if ok {
		*quarantined = append(*quarantined, q)
	}
	return nil, true
}

func buildSecretsIAMIndex(envelopes []facts.Envelope) (secretsIAMIndex, []quarantinedFact, error) {
	index := secretsIAMIndex{
		serviceAccounts: map[string][]secretsIAMServiceAccount{},
		workloads:       map[string][]secretsIAMWorkload{},
		irsa:            map[string][]secretsIAMIRSA{},
		vaultRoles:      map[string][]secretsIAMVaultRole{},
		iamPrincipals:   map[string][]secretsIAMPrincipal{},
		iamTrusts:       map[string][]facts.Envelope{},
		vaultPolicies:   map[string][]secretsIAMVaultPolicy{},
		vaultKV:         map[string][]secretsIAMVaultKV{},
		gcpPrincipals:   map[string][]facts.Envelope{},
		gcpTrusts:       map[string][]facts.Envelope{},
		gcpK8sBindings:  map[string][]secretsIAMGCPBinding{},
		gcpPermissions:  map[string][]facts.Envelope{},
	}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		var fatal error
		switch envelope.FactKind {
		case facts.KubernetesServiceAccountFactKind:
			fatal = indexKubernetesServiceAccount(&index, envelope, &quarantined)
		case facts.KubernetesWorkloadIdentityUseFactKind:
			fatal = indexKubernetesWorkloadIdentityUse(&index, envelope, &quarantined)
		case facts.EKSIRSAAnnotationFactKind:
			fatal = indexEKSIRSAAnnotation(&index, envelope, &quarantined)
		case facts.EKSPodIdentityAssociationFactKind:
			fatal = indexEKSPodIdentityAssociation(&index, envelope, &quarantined)
		case facts.VaultAuthRoleFactKind:
			fatal = indexVaultAuthRole(&index, envelope, &quarantined)
		case facts.AWSIAMPrincipalFactKind:
			fatal = indexAWSIAMPrincipal(&index, envelope, &quarantined)
		case facts.AWSIAMTrustPolicyFactKind:
			addByKey(index.iamTrusts, payloadString(envelope.Payload, "role_arn"), envelope)
		case facts.VaultACLPolicyFactKind:
			fatal = indexVaultACLPolicy(&index, envelope, &quarantined)
		case facts.VaultKVMetadataFactKind:
			fatal = indexVaultKVMetadata(&index, envelope, &quarantined)
		case facts.GCPIAMPrincipalFactKind:
			// deferred: gcp_iam lane, Wave 4d types vault/k8s only.
			addByKey(index.gcpPrincipals, payloadString(envelope.Payload, "principal_fingerprint"), envelope)
		case facts.GCPIAMTrustPolicyFactKind:
			// deferred: gcp_iam lane, Wave 4d types vault/k8s only.
			addByKey(index.gcpTrusts, payloadString(envelope.Payload, "target_service_account_email_digest"), envelope)
		case facts.KubernetesGCPWorkloadIdentityBindingFactKind:
			fatal = indexKubernetesGCPWorkloadIdentityBinding(&index, envelope, &quarantined)
		case facts.GCPIAMPermissionPolicyFactKind:
			// deferred: gcp_iam lane, Wave 4d types vault/k8s only.
			addByKey(index.gcpPermissions, payloadString(envelope.Payload, "principal_fingerprint"), envelope)
		case facts.SecretsIAMCoverageWarningFactKind:
			index.coverage = append(index.coverage, envelope)
		}
		if fatal != nil {
			return secretsIAMIndex{}, nil, fatal
		}
	}
	return index, quarantined, nil
}

// indexKubernetesServiceAccount decodes and indexes one k8s_service_account
// envelope, returning a non-nil error only when the decode failure is FATAL
// (not classified input_invalid). A quarantinable failure is recorded onto
// *quarantined and this returns nil so the caller's loop continues normally.
func indexKubernetesServiceAccount(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeKubernetesServiceAccount(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	addServiceAccountByKey(index.serviceAccounts, decoded.ServiceAccountJoinKey, secretsIAMServiceAccount{env: envelope, decoded: decoded})
	return nil
}

// indexKubernetesWorkloadIdentityUse mirrors indexKubernetesServiceAccount
// for k8s_workload_identity_use envelopes.
func indexKubernetesWorkloadIdentityUse(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeKubernetesWorkloadIdentityUse(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	addWorkloadByKey(index.workloads, decoded.ServiceAccountJoinKey, secretsIAMWorkload{env: envelope, decoded: decoded})
	return nil
}

// indexEKSIRSAAnnotation mirrors indexKubernetesServiceAccount for
// eks_irsa_annotation envelopes.
func indexEKSIRSAAnnotation(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeEKSIRSAAnnotation(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	addIRSAByKey(index.irsa, decoded.ServiceAccountJoinKey, secretsIAMIRSA{
		env:                           envelope,
		roleARN:                       decoded.RoleARN,
		webIdentitySubjectFingerprint: stringOrEmpty(decoded.WebIdentitySubjectFingerprint),
	})
	return nil
}

// indexEKSPodIdentityAssociation mirrors indexKubernetesServiceAccount for
// eks_pod_identity_association envelopes. EKS Pod Identity associations carry
// no web-identity subject; exactPodIdentityTrust never reads
// webIdentitySubjectFingerprint.
func indexEKSPodIdentityAssociation(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeEKSPodIdentityAssociation(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	addIRSAByKey(index.irsa, decoded.ServiceAccountJoinKey, secretsIAMIRSA{
		env:     envelope,
		roleARN: decoded.RoleARN,
	})
	return nil
}

// indexVaultAuthRole decodes and indexes one vault_auth_role envelope. It
// always appends the decoded role to index.vaultAuthRoles (consumed by
// secretsIAMWildcardVaultAuthRoleObservations regardless of selector shape),
// then additionally indexes it by each of its bound service-account join keys
// unless its selector is a wildcard.
func indexVaultAuthRole(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeVaultAuthRole(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	role := secretsIAMVaultRole{env: envelope, decoded: decoded}
	index.vaultAuthRoles = append(index.vaultAuthRoles, role)
	if boolOrFalse(decoded.BoundServiceAccountSelectorWildcard) {
		return nil
	}
	for _, key := range decoded.BoundServiceAccountJoinKeys {
		addVaultRoleByKey(index.vaultRoles, key, role)
	}
	return nil
}

// indexAWSIAMPrincipal decodes and indexes one aws_iam_principal envelope
// (the #4568 AWS IAM lane, unchanged by this wave). MANDATORY fatal
// passthrough: partitionDecodeFailures returns a non-nil fatal for any decode
// error it did NOT classify input_invalid (a future schema-mismatch/
// unsupported-major class, or a non-*factDecodeError). Such an error is
// terminal but is NOT a per-fact quarantine — it must fail the whole
// trust-chain work item so the durable queue triages it, exactly like every
// other decode call site in this file. Swallowing it here (the old
// `continue`) would let a malformed principal Ack as success — the "swallow
// failures" hole the redesign exists to close.
func indexAWSIAMPrincipal(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	principal, err := decodeAWSIAMPrincipal(envelope)
	if err != nil {
		q, ok, fatal := partitionDecodeFailures(envelope, err)
		if fatal != nil {
			return fatal
		}
		if ok {
			*quarantined = append(*quarantined, q)
		}
		return nil
	}
	key := strings.TrimSpace(payloadString(envelope.Payload, "principal_arn"))
	if key == "" {
		return nil
	}
	index.iamPrincipals[key] = append(index.iamPrincipals[key], secretsIAMPrincipal{env: envelope, decoded: principal})
	return nil
}

// indexVaultACLPolicy mirrors indexKubernetesServiceAccount for
// vault_acl_policy envelopes.
func indexVaultACLPolicy(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeVaultACLPolicy(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	addVaultPolicyByKey(index.vaultPolicies, decoded.PolicyJoinKey, secretsIAMVaultPolicy{env: envelope, decoded: decoded})
	return nil
}

// indexVaultKVMetadata mirrors indexKubernetesServiceAccount for
// vault_kv_metadata envelopes.
func indexVaultKVMetadata(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeVaultKVMetadata(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	addVaultKVByKey(index.vaultKV, decoded.KVPathFingerprint, secretsIAMVaultKV{env: envelope, decoded: decoded})
	return nil
}

// indexKubernetesGCPWorkloadIdentityBinding mirrors
// indexKubernetesServiceAccount for k8s_gcp_workload_identity_binding
// envelopes. This K8S-lane kind decodes through the typed seam even though
// its downstream join partner (gcp_iam_trust_policy) stays raw — deferred:
// gcp_iam lane, Wave 4d types vault/k8s only.
func indexKubernetesGCPWorkloadIdentityBinding(index *secretsIAMIndex, envelope facts.Envelope, quarantined *[]quarantinedFact) error {
	decoded, err := decodeKubernetesGCPWorkloadIdentityBinding(envelope)
	if err != nil {
		fatal, skip := quarantineOrFatal(envelope, err, quarantined)
		if fatal != nil || skip {
			return fatal
		}
	}
	addGCPBindingByKey(index.gcpK8sBindings, decoded.ServiceAccountJoinKey, secretsIAMGCPBinding{env: envelope, decoded: decoded})
	return nil
}
