package sbomdocument

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// componentIndexEntry resolves bom-refs and PURLs back to a component ID.
type componentIndexEntry struct {
	componentID string
	bomRef      string
	purl        string
	name        string
	version     string
}

// cycloneDXComponentResult captures envelopes split by kind so the document
// fact can accurately count components versus external references.
type cycloneDXComponentResult struct {
	components   []facts.Envelope
	externalRefs []facts.Envelope
	index        map[string]componentIndexEntry
	warnings     []facts.Envelope
}

func cycloneDXComponentEnvelopes(
	ctx FixtureContext,
	docID string,
	components []cycloneDXComponent,
) cycloneDXComponentResult {
	componentEnvelopes := make([]facts.Envelope, 0, len(components))
	externalRefEnvelopes := make([]facts.Envelope, 0)
	index := make(map[string]componentIndexEntry, len(components))
	identitySeen := make(map[string]string, len(components))
	warnings := make([]facts.Envelope, 0)

	for i, comp := range components {
		identifier := strings.TrimSpace(comp.BOMRef)
		if identifier == "" {
			identifier = fmt.Sprintf("component[%d]", i)
		}
		purl := strings.TrimSpace(comp.PURL)
		name := strings.TrimSpace(comp.Name)
		version := strings.TrimSpace(comp.Version)
		if purl == "" && name == "" {
			warnings = append(warnings, warningFact(ctx, docID, "component:"+identifier, WarningReasonComponentMissingIdentity,
				"cyclonedx component lacks both purl and name"))
			continue
		}

		canonicalKey := componentCanonicalKey(purl, name, version)
		duplicate := false
		if firstID, ok := identitySeen[canonicalKey]; ok {
			duplicate = true
			warnings = append(warnings, warningFact(ctx, docID, "component:"+identifier+":duplicate", WarningReasonDuplicateComponent,
				fmt.Sprintf("cyclonedx component %q duplicates identity already seen as %q", identifier, firstID)))
		}

		componentIDValue := componentID(docID, purl, name, version, identifier)
		if !duplicate {
			identitySeen[canonicalKey] = identifier
		}
		entry := componentIndexEntry{
			componentID: componentIDValue,
			bomRef:      identifier,
			purl:        purl,
			name:        name,
			version:     version,
		}
		if identifier != "" {
			index[identifier] = entry
		}
		if purl != "" {
			if _, exists := index[purl]; !exists {
				index[purl] = entry
			}
		}

		envelope := cycloneDXComponentEnvelope(ctx, docID, componentIDValue, comp, duplicate)
		componentEnvelopes = append(componentEnvelopes, envelope)
		externalRefEnvelopes = append(externalRefEnvelopes, cycloneDXExternalRefEnvelopes(ctx, docID, componentIDValue, comp.ExternalRefs)...)
	}
	return cycloneDXComponentResult{
		components:   componentEnvelopes,
		externalRefs: externalRefEnvelopes,
		index:        index,
		warnings:     warnings,
	}
}

func componentCanonicalKey(purl, name, version string) string {
	if purl != "" {
		return "purl:" + strings.ToLower(purl)
	}
	return "nv:" + strings.ToLower(strings.TrimSpace(name)) + "@" + strings.ToLower(strings.TrimSpace(version))
}

func cycloneDXComponentEnvelope(ctx FixtureContext, docID, componentID string, comp cycloneDXComponent, duplicate bool) facts.Envelope {
	hashes := map[string]string{}
	for _, h := range comp.Hashes {
		alg := strings.TrimSpace(h.Alg)
		val := strings.TrimSpace(h.Content)
		if alg == "" || val == "" {
			continue
		}
		hashes[alg] = strings.ToLower(val)
	}
	licenses := cycloneDXLicenses(comp.Licenses)
	supplierName, supplierURL := cycloneDXSupplierFields(comp.Supplier)

	payload := map[string]any{
		"document_id":   docID,
		"component_id":  componentID,
		"bom_ref":       strings.TrimSpace(comp.BOMRef),
		"name":          strings.TrimSpace(comp.Name),
		"group":         strings.TrimSpace(comp.Group),
		"version":       strings.TrimSpace(comp.Version),
		"type":          strings.TrimSpace(comp.Type),
		"purl":          strings.TrimSpace(comp.PURL),
		"package_id":    canonicalPackageIDFromPURL(comp.PURL),
		"cpe":           strings.TrimSpace(comp.CPE),
		"description":   strings.TrimSpace(comp.Description),
		"publisher":     strings.TrimSpace(comp.Publisher),
		"scope":         strings.TrimSpace(comp.Scope),
		"hashes":        sortHashEntries(hashes),
		"licenses":      licenses,
		"supplier_name": supplierName,
		"supplier_url":  supplierURL,
		"is_duplicate":  duplicate,
		"correlation_anchors": uniqueSorted(nonEmptyStrings(
			strings.TrimSpace(comp.PURL),
			strings.TrimSpace(comp.CPE),
			strings.TrimSpace(comp.BOMRef),
		)),
	}
	stableKey := facts.StableID(facts.SBOMComponentFactKind, map[string]any{
		"component_id": componentID,
		"document_id":  docID,
	})
	return newEnvelope(ctx, facts.SBOMComponentFactKind, stableKey, componentID, payload)
}

func cycloneDXLicenses(licenses []cycloneDXLicense) []map[string]string {
	out := make([]map[string]string, 0, len(licenses))
	for _, lic := range licenses {
		entry := map[string]string{}
		if expr := strings.TrimSpace(lic.Expression); expr != "" {
			entry["expression"] = expr
		}
		if lic.License != nil {
			if id := strings.TrimSpace(lic.License.ID); id != "" {
				entry["id"] = id
			}
			if name := strings.TrimSpace(lic.License.Name); name != "" {
				entry["name"] = name
			}
			if u := strings.TrimSpace(lic.License.URL); u != "" {
				entry["url"] = u
			}
		}
		if len(entry) > 0 {
			out = append(out, entry)
		}
	}
	return sortLicenseEntries(out)
}

func cycloneDXSupplierFields(supplier *cycloneDXSupplier) (string, string) {
	if supplier == nil {
		return "", ""
	}
	name := strings.TrimSpace(supplier.Name)
	url := ""
	for _, u := range supplier.URL {
		if trimmed := strings.TrimSpace(u); trimmed != "" {
			url = trimmed
			break
		}
	}
	return name, url
}

func cycloneDXExternalRefEnvelopes(ctx FixtureContext, docID, componentID string, refs []cycloneDXExternalRef) []facts.Envelope {
	out := make([]facts.Envelope, 0, len(refs))
	for _, ref := range refs {
		refType := strings.TrimSpace(ref.Type)
		refURL := strings.TrimSpace(ref.URL)
		if refType == "" || refURL == "" {
			continue
		}
		out = append(out, externalReferenceFact(ctx, docID, componentID, refType, refURL, ""))
	}
	return sortExternalRefEnvelopes(out)
}

func cycloneDXDependencyEnvelopes(
	ctx FixtureContext,
	docID string,
	deps []cycloneDXDependency,
	index map[string]componentIndexEntry,
) ([]facts.Envelope, []facts.Envelope) {
	envelopes := make([]facts.Envelope, 0)
	warnings := make([]facts.Envelope, 0)
	for _, dep := range deps {
		fromID, fromOK := lookupComponent(index, dep.Ref)
		if !fromOK {
			warnings = append(warnings, warningFact(ctx, docID, "dependency:"+dep.Ref, WarningReasonUnattachedRelationship,
				fmt.Sprintf("cyclonedx dependency ref %q does not resolve to a known component", dep.Ref)))
			continue
		}
		for _, target := range dep.DependsOn {
			toID, ok := lookupComponent(index, target)
			if !ok {
				warnings = append(warnings, warningFact(ctx, docID, "dependency:"+dep.Ref+"->"+target, WarningReasonUnattachedRelationship,
					fmt.Sprintf("cyclonedx dependency target %q does not resolve to a known component", target)))
				continue
			}
			envelopes = append(envelopes, dependencyFact(ctx, docID, fromID, toID, "DEPENDS_ON", "cyclonedx.dependency"))
		}
	}
	return envelopes, warnings
}

func lookupComponent(index map[string]componentIndexEntry, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	if entry, ok := index[ref]; ok {
		return entry.componentID, true
	}
	return "", false
}

func cycloneDXUnsupportedWarnings(ctx FixtureContext, docID string, doc cycloneDXDocument) []facts.Envelope {
	out := make([]facts.Envelope, 0)
	if len(doc.Vulnerabilities) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:vulnerabilities", WarningReasonUnsupportedField,
			fmt.Sprintf("cyclonedx vulnerabilities section ignored (%d entries); use advisory collectors", len(doc.Vulnerabilities))))
	}
	if len(doc.Services) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:services", WarningReasonUnsupportedField,
			fmt.Sprintf("cyclonedx services section ignored (%d entries)", len(doc.Services))))
	}
	if len(doc.Compositions) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:compositions", WarningReasonUnsupportedField,
			fmt.Sprintf("cyclonedx compositions section ignored (%d entries)", len(doc.Compositions))))
	}
	if len(doc.Formulation) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:formulation", WarningReasonUnsupportedField,
			fmt.Sprintf("cyclonedx formulation section ignored (%d entries)", len(doc.Formulation))))
	}
	if len(doc.Annotations) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:annotations", WarningReasonUnsupportedField,
			fmt.Sprintf("cyclonedx annotations section ignored (%d entries)", len(doc.Annotations))))
	}
	return out
}

func cycloneDXSubjectWarnings(ctx FixtureContext, docID string, subjects []string, ambiguous bool) []facts.Envelope {
	if ambiguous {
		return []facts.Envelope{
			warningFact(ctx, docID, "subject:ambiguous", WarningReasonAmbiguousSubject,
				fmt.Sprintf("cyclonedx document reports %d distinct subject digests", len(subjects))),
		}
	}
	if len(subjects) == 0 {
		return []facts.Envelope{
			warningFact(ctx, docID, "subject:missing", WarningReasonMissingSubject,
				"cyclonedx document parsed without a metadata.component subject digest"),
		}
	}
	return nil
}
