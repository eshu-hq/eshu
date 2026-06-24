// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// spdxComponentResult bundles per-package projection output so the document
// fact and dependency resolver can read counts and subject digests separately.
type spdxComponentResult struct {
	components     []facts.Envelope
	externalRefs   []facts.Envelope
	index          map[string]componentIndexEntry
	warnings       []facts.Envelope
	subjectDigests []string
}

func spdxComponentEnvelopes(
	ctx FixtureContext,
	docID string,
	packages []spdxPackage,
	subjectSPDXIDs []string,
) spdxComponentResult {
	subjectIDs := make(map[string]struct{}, len(subjectSPDXIDs))
	for _, id := range subjectSPDXIDs {
		subjectIDs[strings.TrimSpace(id)] = struct{}{}
	}
	componentEnvelopes := make([]facts.Envelope, 0, len(packages))
	externalRefEnvelopes := make([]facts.Envelope, 0)
	index := make(map[string]componentIndexEntry, len(packages))
	identitySeen := make(map[string]string, len(packages))
	warnings := make([]facts.Envelope, 0)
	subjectDigests := make([]string, 0)

	for i, pkg := range packages {
		spdxID := strings.TrimSpace(pkg.SPDXID)
		identifier := spdxID
		if identifier == "" {
			identifier = fmt.Sprintf("package[%d]", i)
		}
		purl := spdxPackagePURL(pkg)
		cpe := spdxPackageCPE(pkg)
		name := strings.TrimSpace(pkg.Name)
		version := strings.TrimSpace(pkg.VersionInfo)
		if purl == "" && name == "" {
			warnings = append(warnings, warningFact(ctx, docID, "package:"+identifier, WarningReasonComponentMissingIdentity,
				"spdx package lacks both PURL external ref and name"))
			continue
		}

		canonicalKey := componentCanonicalKey(purl, name, version)
		duplicate := false
		if firstID, ok := identitySeen[canonicalKey]; ok {
			duplicate = true
			warnings = append(warnings, warningFact(ctx, docID, "package:"+identifier+":duplicate", WarningReasonDuplicateComponent,
				fmt.Sprintf("spdx package %q duplicates identity already seen as %q", identifier, firstID)))
		}

		componentIDValue := componentID(docID, purl, name, version, identifier)
		if !duplicate {
			identitySeen[canonicalKey] = identifier
		}

		entry := componentIndexEntry{
			componentID: componentIDValue,
			bomRef:      spdxID,
			purl:        purl,
			name:        name,
			version:     version,
		}
		if spdxID != "" {
			index[spdxID] = entry
		}
		if purl != "" {
			if _, exists := index[purl]; !exists {
				index[purl] = entry
			}
		}

		envelope := spdxComponentEnvelope(ctx, docID, componentIDValue, pkg, purl, cpe, duplicate)
		componentEnvelopes = append(componentEnvelopes, envelope)
		externalRefEnvelopes = append(externalRefEnvelopes, spdxExternalRefEnvelopes(ctx, docID, componentIDValue, pkg.ExternalRefs)...)

		if _, isSubject := subjectIDs[spdxID]; isSubject {
			for _, sum := range pkg.Checksums {
				alg := strings.TrimSpace(sum.Algorithm)
				val := strings.TrimSpace(sum.ChecksumValue)
				if alg == "" || val == "" {
					continue
				}
				subjectDigests = append(subjectDigests, normalizeHashDigest(alg, val))
			}
		}
	}

	return spdxComponentResult{
		components:     componentEnvelopes,
		externalRefs:   externalRefEnvelopes,
		index:          index,
		warnings:       warnings,
		subjectDigests: uniqueSorted(subjectDigests),
	}
}

func spdxComponentEnvelope(ctx FixtureContext, docID, componentID string, pkg spdxPackage, purl, cpe string, duplicate bool) facts.Envelope {
	hashes := map[string]string{}
	for _, sum := range pkg.Checksums {
		alg := strings.TrimSpace(sum.Algorithm)
		val := strings.TrimSpace(sum.ChecksumValue)
		if alg == "" || val == "" {
			continue
		}
		hashes[alg] = strings.ToLower(val)
	}

	licenses := spdxLicenses(pkg)
	supplierName, supplierKind := spdxSupplierParts(pkg.Supplier)

	payload := map[string]any{
		"document_id":         docID,
		"component_id":        componentID,
		"spdx_id":             strings.TrimSpace(pkg.SPDXID),
		"name":                strings.TrimSpace(pkg.Name),
		"version":             strings.TrimSpace(pkg.VersionInfo),
		"type":                spdxComponentType(pkg),
		"purl":                purl,
		"package_id":          canonicalPackageIDFromPURL(purl),
		"cpe":                 cpe,
		"description":         strings.TrimSpace(firstNonBlank(pkg.Description, pkg.Summary)),
		"publisher":           strings.TrimSpace(pkg.Originator),
		"download_location":   strings.TrimSpace(pkg.DownloadLocation),
		"homepage":            strings.TrimSpace(pkg.HomePage),
		"hashes":              sortHashEntries(hashes),
		"licenses":            licenses,
		"supplier_name":       supplierName,
		"supplier_kind":       supplierKind,
		"copyright":           strings.TrimSpace(pkg.Copyright),
		"is_duplicate":        duplicate,
		"correlation_anchors": uniqueSorted(nonEmptyStrings(purl, cpe, strings.TrimSpace(pkg.SPDXID))),
	}
	stableKey := facts.StableID(facts.SBOMComponentFactKind, map[string]any{
		"component_id": componentID,
		"document_id":  docID,
	})
	return newEnvelope(ctx, facts.SBOMComponentFactKind, stableKey, componentID, payload)
}

func spdxPackagePURL(pkg spdxPackage) string {
	for _, ref := range pkg.ExternalRefs {
		if strings.EqualFold(strings.TrimSpace(ref.ReferenceCategory), "PACKAGE-MANAGER") &&
			strings.EqualFold(strings.TrimSpace(ref.ReferenceType), "purl") {
			return strings.TrimSpace(ref.ReferenceLocator)
		}
	}
	return ""
}

func spdxPackageCPE(pkg spdxPackage) string {
	for _, ref := range pkg.ExternalRefs {
		if !strings.EqualFold(strings.TrimSpace(ref.ReferenceCategory), "SECURITY") {
			continue
		}
		refType := strings.ToLower(strings.TrimSpace(ref.ReferenceType))
		if refType == "cpe23type" || refType == "cpe22type" {
			return strings.TrimSpace(ref.ReferenceLocator)
		}
	}
	return ""
}

func spdxComponentType(pkg spdxPackage) string {
	if purpose := strings.TrimSpace(pkg.PrimaryPackagePurpose); purpose != "" {
		return strings.ToLower(purpose)
	}
	return "library"
}

func spdxLicenses(pkg spdxPackage) []map[string]string {
	out := make([]map[string]string, 0)
	for _, raw := range []string{pkg.LicenseDeclared, pkg.LicenseConcluded} {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.EqualFold(trimmed, "NOASSERTION") || strings.EqualFold(trimmed, "NONE") {
			continue
		}
		out = append(out, map[string]string{"expression": trimmed})
	}
	for _, raw := range pkg.LicenseInfoFromFiles {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.EqualFold(trimmed, "NOASSERTION") {
			continue
		}
		out = append(out, map[string]string{"id": trimmed})
	}
	return sortLicenseEntries(out)
}

func spdxSupplierParts(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "NOASSERTION") {
		return "", ""
	}
	if idx := strings.Index(raw, ":"); idx > 0 {
		kind := strings.ToLower(strings.TrimSpace(raw[:idx]))
		name := strings.TrimSpace(raw[idx+1:])
		return name, kind
	}
	return raw, ""
}

func spdxExternalRefEnvelopes(ctx FixtureContext, docID, componentID string, refs []spdxExternalRef) []facts.Envelope {
	out := make([]facts.Envelope, 0, len(refs))
	for _, ref := range refs {
		refType := strings.TrimSpace(ref.ReferenceType)
		refLocator := strings.TrimSpace(ref.ReferenceLocator)
		if refType == "" || refLocator == "" {
			continue
		}
		out = append(out, externalReferenceFact(ctx, docID, componentID, refType, "", refLocator))
	}
	return sortExternalRefEnvelopes(out)
}

func spdxRelationshipEnvelopes(
	ctx FixtureContext,
	docID string,
	rels []spdxRelationship,
	index map[string]componentIndexEntry,
) ([]facts.Envelope, []facts.Envelope) {
	envelopes := make([]facts.Envelope, 0)
	warnings := make([]facts.Envelope, 0)
	for _, rel := range rels {
		relType := strings.ToUpper(strings.TrimSpace(rel.RelationshipType))
		if relType == "" {
			continue
		}
		from := strings.TrimSpace(rel.SPDXElementID)
		to := strings.TrimSpace(rel.RelatedSPDXElement)
		// Skip describes — it is captured as the document subject.
		if relType == "DESCRIBES" || relType == "DESCRIBED_BY" {
			continue
		}
		fromID, fromOK := lookupComponent(index, from)
		toID, toOK := lookupComponent(index, to)
		if !fromOK || !toOK {
			warnings = append(warnings, warningFact(ctx, docID, "relationship:"+from+"->"+to, WarningReasonUnattachedRelationship,
				fmt.Sprintf("spdx relationship %s %s %s did not resolve to known packages", from, relType, to)))
			continue
		}
		envelopes = append(envelopes, dependencyFact(ctx, docID, fromID, toID, relType, "spdx.relationship"))
	}
	return envelopes, warnings
}
