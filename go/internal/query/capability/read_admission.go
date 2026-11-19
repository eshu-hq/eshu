// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capability

import (
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// CapabilityReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the capability catalog read (GET
// /api/v0/capabilities). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned static catalog
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const CapabilityReadAdmissionContractVersion = "v0"

// CapabilityReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of calling the static catalog
// load directly. It re-declares exactly the one read the capability
// route admits through — the embedded catalog load — so the boundary
// is held honest by the compile-time adapter assertion below plus the
// frozen method-set contract test. It will back the eshu-api
// extraction handoff for this read. The seam is read-only on purpose:
// the declared method is a bounded static read and the port carries no
// mutation surface, so a dest consumer implementing only this contract
// can serve the capability read but cannot acquire a write it did not
// bargain for through it.
//
// W-wave consumer: W5 capabilities (the single oracle Capabilities
// row; the eshu-mcp satellite probes this route to admit forwarded
// credentials, eshu-hq/eshu-mcp#50).
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the staying static signature whole in this
// capability-scoped shape instead of aliasing the catalog load, so no
// other family can cite this pin — a blanket pin is forbidden, and
// the widened shape needs arbiter review before satellite
// consumption.
type CapabilityReadAdmission interface {
	LoadCapabilityCatalog() (capabilitycatalog.Catalog, error)
}

// capabilityStaticLoader is the minimal static satisfier behind the
// pin: it delegates the re-declared read to the static catalog load
// the handler leg calls today. The delegation keeps the pin honest in
// one direction: removal, rename, or re-signing of Load fails
// compilation here. A new catalog accessor in that package does not
// widen this pin — adoption takes deliberate re-declaration plus a
// frozen-list update. It pins no shared port — the delegation names
// only the load this pin mirrors, and no other family may cite this
// pin as coverage.
type capabilityStaticLoader struct{}

func (capabilityStaticLoader) LoadCapabilityCatalog() (capabilitycatalog.Catalog, error) {
	return capabilitycatalog.Load()
}

var _ CapabilityReadAdmission = capabilityStaticLoader{}
