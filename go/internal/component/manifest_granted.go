// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// LoadGrantedManifest loads and validates the manifest at path against this
// registry's durable producer grants. It is the loader for read-only commands
// that judge a manifest without installing it: inspect, verify, and
// conformance.
//
// The registry is consulted only when the manifest declares a core-owned fact
// kind, because only those declarations need a grant. A manifest that
// declares namespaced kinds only never reads the registry, so an absent or
// unreadable registry cannot change its verdict. A core-owned declaration
// passes only when a live grant names this producer, version, kind, and
// schema version, scoped to one of the manifest's collector kinds; anything
// else fails closed with the same error LoadManifest returns. An empty home
// holds no grants, so a core-owned declaration fails closed there too.
//
// The returned grants are the set the manifest was validated against, nil
// when the registry was not consulted. Pass them to Policy.VerifyWithGrants
// so the verdict matches the load.
func (r Registry) LoadGrantedManifest(path string) (Manifest, []ProducerGrant, error) {
	return r.LoadGrantedManifestAt(path, time.Now().UTC())
}

// LoadGrantedManifestAt loads and validates a manifest against one durable
// producer-grant snapshot evaluated at now. Callers that pass the grants on to
// another verifier must use the same instant for every authorization decision.
func (r Registry) LoadGrantedManifestAt(path string, now time.Time) (Manifest, []ProducerGrant, error) {
	manifest, err := decodeManifest(path)
	if err != nil {
		return Manifest{}, nil, err
	}
	var grants []ProducerGrant
	if manifest.declaresCoreFactKind() && r.hasHome() {
		grants, err = r.ProducerGrants()
		if err != nil {
			return Manifest{}, nil, err
		}
	}
	if err := manifest.validateFactFamilies(grants, now); err != nil {
		return Manifest{}, nil, WrapError(ErrorCodeInvalidManifest, err.Error(), err)
	}
	return manifest, grants, nil
}

// declaresCoreFactKind reports whether any emitted fact family names a
// core-owned fact kind, which makes the manifest depend on producer grants.
func (m Manifest) declaresCoreFactKind() bool {
	for _, fact := range m.Spec.EmittedFacts {
		if facts.IsCoreFactKind(fact.Kind) {
			return true
		}
	}
	return false
}
