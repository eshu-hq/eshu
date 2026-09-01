// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"

	factschema "github.com/eshu-hq/eshu/sdk/go/factschema"
)

// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the 63 root call sites keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage.

var (
	serviceCatalogDecodeQuarantine = schemadecode.ServiceCatalogDecodeQuarantine
	decodeCodeDataflowFunction     = schemadecode.DecodeCodeDataflowFunction
	decodeCodeDataflowScanned      = schemadecode.DecodeCodeDataflowScanned
)

// factschemaEnvelope forwards to [schemadecode.FactschemaEnvelope]. It is a func
// rather than a var binding so its two root call sites keep inlining it; the
// decoder forwarders above are var bindings because their targets are far too
// large to inline in any form, so the binding form costs them nothing.
func factschemaEnvelope(env facts.Envelope) factschema.Envelope {
	return schemadecode.FactschemaEnvelope(env)
}
