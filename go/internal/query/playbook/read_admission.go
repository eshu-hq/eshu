// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

// PlaybookReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the query-playbook reads (the catalog
// list and the resolver). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned static catalog
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const PlaybookReadAdmissionContractVersion = "v0"

// PlaybookReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of calling the static catalog
// directly. It re-declares exactly the three reads the playbook routes
// admit through — the catalog list, the playbook lookup, and the
// playbook resolve — so the boundary is held honest by the
// compile-time adapter assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// these reads. The seam is read-only on purpose: every declared
// method is a bounded static read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can
// serve the playbook reads but cannot acquire a write it did not
// bargain for through it.
//
// W-wave consumer: W5 playbooks (both oracle Playbooks rows).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the staying static signatures whole in this
// playbook-scoped shape instead of aliasing the catalog surface, so
// no other family can cite this pin — a blanket pin is forbidden, and
// the widened shape needs arbiter review before satellite
// consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The catalog versions list (CatalogVersions) is a schema marker,
//     not a route read: no leg calls it, so there is no method set to
//     freeze for it.
//   - The limit/offset/view parsing, the 400/404 mappings, and the
//     truth envelope are handler behavior, not port shape: there is no
//     method set to freeze for them.
type PlaybookReadAdmission interface {
	ListPlaybookDefinitions() []Definition
	LookupPlaybook(string) (Definition, bool)
	ResolvePlaybook(Definition, map[string]string) (ResolvedPlaybook, error)
}

// playbookStaticCatalog is the minimal static satisfier behind the
// pin: it delegates each re-declared read to the static catalog
// function the handler legs call today. The delegation keeps the pin
// honest in one direction: removal, rename, or re-signing of any of
// the three functions fails compilation here. A new catalog accessor
// in this package does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the delegation names only the three functions this pin mirrors, and
// no other family may cite this pin as coverage.
type playbookStaticCatalog struct{}

func (playbookStaticCatalog) ListPlaybookDefinitions() []Definition {
	return Catalog()
}

func (playbookStaticCatalog) LookupPlaybook(id string) (Definition, bool) {
	return Lookup(id)
}

func (playbookStaticCatalog) ResolvePlaybook(pb Definition, inputs map[string]string) (ResolvedPlaybook, error) {
	return pb.Resolve(inputs)
}

var _ PlaybookReadAdmission = playbookStaticCatalog{}
