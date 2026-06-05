package reducer

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

func supplyChainCVEFromEnvelope(envelope facts.Envelope) supplyChainImpactCVE {
	return supplyChainImpactCVE{
		factID:          envelope.FactID,
		cveID:           supplyChainCVEID(envelope.Payload),
		advisoryID:      payloadStr(envelope.Payload, "advisory_id"),
		source:          payloadStr(envelope.Payload, "source"),
		cvssScore:       supplyChainFloat(envelope.Payload, "cvss_score"),
		cvssVector:      payloadStr(envelope.Payload, "cvss_vector"),
		severityLabel:   payloadStr(envelope.Payload, "severity_label"),
		publishedAt:     payloadStr(envelope.Payload, "published_at"),
		sourceUpdatedAt: payloadStr(envelope.Payload, "modified_at"),
		withdrawnAt:     payloadStr(envelope.Payload, "withdrawn_at"),
	}
}

func supplyChainAffectedPackageFromEnvelope(envelope facts.Envelope) supplyChainAffectedPackage {
	return supplyChainAffectedPackage{
		factID:           envelope.FactID,
		cveID:            supplyChainCVEID(envelope.Payload),
		source:           payloadStr(envelope.Payload, "source"),
		advisoryID:       payloadStr(envelope.Payload, "advisory_id"),
		packageID:        payloadStr(envelope.Payload, "package_id"),
		ecosystem:        strings.ToLower(payloadStr(envelope.Payload, "ecosystem")),
		name:             payloadStr(envelope.Payload, "package_name"),
		purl:             payloadStr(envelope.Payload, "purl"),
		affectedVersions: payloadStrings(envelope.Payload, "affected_version", "affected_versions"),
		affectedRanges:   supplyChainAffectedRangesFromPayload(envelope.Payload),
		affectedRangeRaw: payloadStr(envelope.Payload, "affected_range"),
		fixedVersions:    payloadStrings(envelope.Payload, "fixed_version", "fixed_versions"),
	}
}

func supplyChainAffectedProductFromEnvelope(envelope facts.Envelope) supplyChainAffectedProduct {
	return supplyChainAffectedProduct{
		factID:          envelope.FactID,
		cveID:           supplyChainCVEID(envelope.Payload),
		criteria:        payloadStr(envelope.Payload, "criteria"),
		matchCriteriaID: payloadStr(envelope.Payload, "match_criteria_id"),
		vulnerable:      payloadBool(envelope.Payload, "vulnerable"),
	}
}

func supplyChainConsumptionFromEnvelope(envelope facts.Envelope) supplyChainPackageConsumption {
	return supplyChainPackageConsumption{
		factID:                    envelope.FactID,
		evidenceKind:              packageConsumptionCorrelationFactKind,
		packageID:                 payloadStr(envelope.Payload, "package_id"),
		repositoryID:              payloadStr(envelope.Payload, "repository_id"),
		dependencyRange:           payloadStr(envelope.Payload, "dependency_range"),
		observedVersion:           firstNonBlank(payloadStr(envelope.Payload, "observed_version"), payloadStr(envelope.Payload, "resolved_version")),
		requestedRange:            payloadStr(envelope.Payload, "requested_range"),
		installedVersion:          payloadStr(envelope.Payload, "installed_version"),
		dependencyPath:            payloadOrderedStrings(envelope.Payload, "dependency_path"),
		dependencyDepth:           supplyChainInt(envelope.Payload, "dependency_depth"),
		directDependency:          payloadBoolPointer(envelope.Payload, "direct_dependency"),
		dependencyScope:           supplyChainDependencyScope(envelope.Payload),
		versionEvidence:           payloadStr(envelope.Payload, "version_evidence"),
		unresolvedMSBuildProperty: payloadStr(envelope.Payload, "unresolved_msbuild_property"),
		ambiguousMSBuildProperty:  payloadStr(envelope.Payload, "ambiguous_msbuild_property"),
		partialEvidence:           payloadBool(envelope.Payload, "partial_evidence"),
		lockfile:                  payloadBool(envelope.Payload, "lockfile"),
	}
}

func supplyChainSBOMComponentFromEnvelope(envelope facts.Envelope) supplyChainSBOMComponent {
	purl := payloadStr(envelope.Payload, "purl")
	return supplyChainSBOMComponent{
		factID:     envelope.FactID,
		documentID: payloadStr(envelope.Payload, "document_id"),
		purl:       purl,
		cpe:        payloadStr(envelope.Payload, "cpe"),
		packageID:  packageIDFromPURL(purl),
		version:    firstNonBlank(payloadStr(envelope.Payload, "version"), versionFromPURL(purl)),
	}
}

func supplyChainOSPackageFromEnvelope(envelope facts.Envelope) supplyChainOSPackage {
	purl := payloadStr(envelope.Payload, "purl")
	return supplyChainOSPackage{
		factID:               envelope.FactID,
		scopeID:              envelope.ScopeID,
		packageID:            packageIDFromPURL(purl),
		purl:                 purl,
		distro:               strings.ToLower(payloadStr(envelope.Payload, "distro")),
		distroVersion:        payloadStr(envelope.Payload, "distro_version"),
		packageManager:       strings.ToLower(payloadStr(envelope.Payload, "package_manager")),
		name:                 payloadStr(envelope.Payload, "name"),
		arch:                 payloadStr(envelope.Payload, "arch"),
		installedVersion:     payloadStr(envelope.Payload, "installed_version_raw"),
		repositoryClass:      strings.ToLower(payloadStr(envelope.Payload, "repository_class")),
		vendorAdvisorySource: strings.ToLower(payloadStr(envelope.Payload, "vendor_advisory_source")),
	}
}

func supplyChainAttachmentFromEnvelope(envelope facts.Envelope) supplyChainAttachment {
	return supplyChainAttachment{
		factID:        envelope.FactID,
		documentID:    payloadStr(envelope.Payload, "document_id"),
		subjectDigest: payloadStr(envelope.Payload, "subject_digest"),
		status:        payloadStr(envelope.Payload, "attachment_status"),
	}
}

func supplyChainImageIdentityFromEnvelope(envelope facts.Envelope) supplyChainImageIdentity {
	return supplyChainImageIdentity{
		factID:          envelope.FactID,
		digest:          payloadStr(envelope.Payload, "digest"),
		imageRef:        payloadStr(envelope.Payload, "image_ref"),
		repositoryID:    payloadStr(envelope.Payload, "repository_id"),
		outcome:         payloadStr(envelope.Payload, "outcome"),
		canonicalWrites: supplyChainInt(envelope.Payload, "canonical_writes"),
	}
}

func supplyChainDeploymentContextFromEnvelope(envelope facts.Envelope) supplyChainDeploymentContext {
	return supplyChainDeploymentContext{
		factID:         envelope.FactID,
		artifactDigest: payloadStr(envelope.Payload, "artifact_digest"),
		imageRef:       payloadStr(envelope.Payload, "image_ref"),
		repositoryID:   payloadStr(envelope.Payload, "repository_id"),
		environment:    payloadStr(envelope.Payload, "environment"),
		outcome:        payloadStr(envelope.Payload, "outcome"),
	}
}

func supplyChainWorkloadContextsFromEnvelope(envelope facts.Envelope) []supplyChainWorkloadContext {
	repositoryID := supplyChainWorkloadRepositoryID(envelope)
	workloadIDs := supplyChainWorkloadIDsFromPayload(envelope.Payload)
	if repositoryID == "" || len(workloadIDs) == 0 {
		return nil
	}
	out := make([]supplyChainWorkloadContext, 0, len(workloadIDs))
	for _, workloadID := range workloadIDs {
		out = append(out, supplyChainWorkloadContext{
			factID:       envelope.FactID,
			repositoryID: repositoryID,
			workloadID:   workloadID,
		})
	}
	return out
}

func supplyChainWorkloadRepositoryID(envelope facts.Envelope) string {
	direct := firstNonBlank(
		payloadStr(envelope.Payload, "repository_id"),
		payloadStr(envelope.Payload, "repo_id"),
	)
	if direct != "" {
		return direct
	}
	scoped := firstNonBlank(
		payloadStr(envelope.Payload, "scope_id"),
		envelope.ScopeID,
	)
	if repositoryID := repositoryIDFromReducerScope(scoped); repositoryID != "" {
		return repositoryID
	}
	for _, scopeID := range payloadOrderedStrings(envelope.Payload, "related_scope_ids") {
		if repositoryID := repositoryIDFromReducerScope(scopeID); repositoryID != "" {
			return repositoryID
		}
	}
	return strings.TrimSpace(scoped)
}

func repositoryIDFromReducerScope(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if strings.HasPrefix(scopeID, "repository:") {
		return scopeID
	}
	if strings.HasPrefix(scopeID, "git-repository-scope:") {
		return strings.TrimSpace(strings.TrimPrefix(scopeID, "git-repository-scope:"))
	}
	return ""
}

func supplyChainWorkloadIDsFromPayload(payload map[string]any) []string {
	var workloadIDs []string
	if workloadID := payloadStr(payload, "workload_id"); workloadID != "" {
		workloadIDs = append(workloadIDs, workloadID)
	}
	for _, entityKey := range payloadOrderedStrings(payload, "entity_keys") {
		if strings.HasPrefix(entityKey, "workload:") {
			workloadIDs = append(workloadIDs, entityKey)
		}
	}
	return uniqueSortedStrings(workloadIDs)
}

func supplyChainServiceContextFromEnvelope(envelope facts.Envelope) supplyChainServiceContext {
	return supplyChainServiceContext{
		factID:         envelope.FactID,
		repositoryID:   payloadStr(envelope.Payload, "repository_id"),
		serviceID:      payloadStr(envelope.Payload, "service_id"),
		workloadID:     payloadStr(envelope.Payload, "workload_id"),
		outcome:        payloadStr(envelope.Payload, "outcome"),
		driftStatus:    payloadStr(envelope.Payload, "drift_status"),
		provenanceOnly: payloadBool(envelope.Payload, "provenance_only"),
	}
}

func supplyChainDependencyScope(payload map[string]any) string {
	if scope := payloadStr(payload, "dependency_scope"); scope != "" {
		return scope
	}
	return payloadStr(payload, "manifest_section")
}

func firstConsumption(
	packageID string,
	consumption map[string][]supplyChainPackageConsumption,
) supplyChainPackageConsumption {
	var fallback supplyChainPackageConsumption
	for _, row := range consumption[packageID] {
		if row.repositoryID == "" {
			continue
		}
		if strings.TrimSpace(row.installedVersion) != "" || row.lockfile {
			return row
		}
		if fallback.repositoryID == "" {
			fallback = row
		}
	}
	return fallback
}

func firstSBOMImpactPath(
	pkg supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) (supplyChainSBOMComponent, supplyChainAttachment, supplyChainImageIdentity, bool, []string) {
	var missing []string
	for _, component := range index.components {
		if !componentMatchesAffectedPackage(component, pkg) {
			continue
		}
		attachment := index.attachments[component.documentID]
		if attachment.subjectDigest == "" || attachment.status == "subject_mismatch" || attachment.status == "unknown_subject" {
			continue
		}
		image := index.images[attachment.subjectDigest]
		if image.digest == "" {
			missing = append(missing, "image identity evidence missing")
			continue
		}
		if reason := unusableSupplyChainImageIdentityReason(image); reason != "" {
			missing = append(missing, reason)
			continue
		}
		return component, attachment, image, true, nil
	}
	return supplyChainSBOMComponent{}, supplyChainAttachment{}, supplyChainImageIdentity{}, false, uniqueSortedStrings(missing)
}

func firstOSPackageImpactPath(
	pkg supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) (supplyChainOSPackage, bool) {
	vendorSource := classifyAffectedPackageAdvisorySource(pkg)
	if vendorSource == "" {
		return supplyChainOSPackage{}, false
	}
	for _, key := range affectedOSPackageLookupKeys(pkg) {
		for _, installed := range index.osPackages[key] {
			if !osPackageMatchesAffectedPackage(installed, pkg, vendorSource) {
				continue
			}
			return installed, true
		}
	}
	return supplyChainOSPackage{}, false
}

func osPackageMatchesAffectedPackage(
	installed supplyChainOSPackage,
	pkg supplyChainAffectedPackage,
	vendorSource string,
) bool {
	if !supportedOSPackageImpactManager(installed.packageManager) ||
		installed.distroVersion == "" || installed.arch == "" {
		return false
	}
	if installed.repositoryClass != "vendor" || installed.vendorAdvisorySource == "" {
		return false
	}
	if installed.vendorAdvisorySource != vendorSource {
		return false
	}
	if !osPackageEcosystemMatchesVendor(pkg.ecosystem, installed.packageManager, installed.vendorAdvisorySource, installed.distro) {
		return false
	}
	if pkg.packageID != "" && pkg.packageID == installed.packageID {
		return true
	}
	return pkg.purl != "" && packageIDFromPURL(pkg.purl) == installed.packageID
}

func supportedOSPackageImpactManager(manager string) bool {
	switch strings.ToLower(strings.TrimSpace(manager)) {
	case "rpm", "dpkg", "apk":
		return true
	default:
		return false
	}
}

func osPackageEcosystemMatchesVendor(
	ecosystem string,
	packageManager string,
	vendorSource string,
	distro string,
) bool {
	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	if ecosystem == "" {
		return true
	}
	packageManager = strings.ToLower(strings.TrimSpace(packageManager))
	vendorSource = strings.ToLower(strings.TrimSpace(vendorSource))
	distro = strings.ToLower(strings.TrimSpace(distro))
	switch ecosystem {
	case packageManager, vendorSource, distro:
		return true
	case string(packageidentity.EcosystemOS):
		return osPackageFamilyFromPackageManager(packageManager) != "" &&
			osPackageVendorMatchesFamily(vendorSource, packageManager, distro)
	case "deb":
		return packageManager == "dpkg" || vendorSource == "debian" || distro == "debian" || distro == "ubuntu"
	case "debian":
		return vendorSource == "debian" || distro == "debian"
	case "ubuntu":
		return vendorSource == "ubuntu" || distro == "ubuntu"
	case "apk", "alpine":
		return packageManager == "apk" || vendorSource == "alpine" || distro == "alpine"
	case "rpm":
		return packageManager == "rpm"
	case "redhat", "rhel", "fedora", "centos", "rocky", "rockylinux", "alma", "amazon", "amazonlinux":
		return packageManager == "rpm" && (vendorSource == ecosystem || distro == ecosystem)
	default:
		return false
	}
}

func firstSBOMProductImpactPath(
	product supplyChainAffectedProduct,
	index supplyChainImpactIndex,
) (supplyChainSBOMComponent, supplyChainAttachment, supplyChainImageIdentity, bool, []string) {
	var missing []string
	for _, component := range index.components {
		if !componentMatchesAffectedProduct(component, product) {
			continue
		}
		attachment := index.attachments[component.documentID]
		if attachment.subjectDigest == "" || attachment.status == "subject_mismatch" || attachment.status == "unknown_subject" {
			continue
		}
		image := index.images[attachment.subjectDigest]
		if image.digest == "" {
			missing = append(missing, "image identity evidence missing")
			continue
		}
		if reason := unusableSupplyChainImageIdentityReason(image); reason != "" {
			missing = append(missing, reason)
			continue
		}
		return component, attachment, image, true, nil
	}
	return supplyChainSBOMComponent{}, supplyChainAttachment{}, supplyChainImageIdentity{}, false, uniqueSortedStrings(missing)
}

func unusableSupplyChainImageIdentityReason(image supplyChainImageIdentity) string {
	switch image.outcome {
	case "", string(ContainerImageIdentityExactDigest), string(ContainerImageIdentityTagResolved):
		if image.outcome == "" || image.canonicalWrites > 0 {
			return ""
		}
		return "image identity evidence missing"
	case string(ContainerImageIdentityAmbiguousTag):
		return "image identity evidence ambiguous"
	case string(ContainerImageIdentityStaleTag):
		return "image identity evidence stale"
	case string(ContainerImageIdentityUnresolved):
		return "image identity evidence unresolved"
	default:
		return "image identity evidence unsupported"
	}
}

func componentMatchesAffectedPackage(component supplyChainSBOMComponent, pkg supplyChainAffectedPackage) bool {
	if pkg.purl != "" && component.purl == pkg.purl {
		return true
	}
	if pkg.packageID != "" && component.packageID == pkg.packageID {
		return true
	}
	return false
}

func componentMatchesAffectedProduct(component supplyChainSBOMComponent, product supplyChainAffectedProduct) bool {
	if product.criteria == "" || component.cpe == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(product.criteria), strings.TrimSpace(component.cpe))
}

func packageIDFromPURL(purl string) string {
	purl = strings.TrimSpace(purl)
	if before, _, ok := strings.Cut(purl, "@"); ok {
		return before
	}
	return purl
}

func versionFromPURL(purl string) string {
	_, after, ok := strings.Cut(strings.TrimSpace(purl), "@")
	if !ok {
		return ""
	}
	if version, _, ok := strings.Cut(after, "?"); ok {
		return version
	}
	return after
}

func versionFromCPE23Criteria(criteria string) string {
	criteria = strings.TrimSpace(criteria)
	if !strings.HasPrefix(criteria, "cpe:2.3:") {
		return ""
	}
	parts := strings.Split(criteria, ":")
	if len(parts) < 6 {
		return ""
	}
	version := strings.TrimSpace(parts[5])
	if version == "*" || version == "-" {
		return ""
	}
	return version
}

func payloadBool(payload map[string]any, key string) bool {
	value, ok := payloadBoolPointerValue(payload, key)
	return ok && value
}

func payloadBoolPointer(payload map[string]any, key string) *bool {
	value, ok := payloadBoolPointerValue(payload, key)
	if !ok {
		return nil
	}
	return &value
}

func payloadBoolPointerValue(payload map[string]any, key string) (bool, bool) {
	switch value := payload[key].(type) {
	case bool:
		return value, true
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return false, false
		}
		return strings.EqualFold(trimmed, "true"), true
	default:
		return false, false
	}
}

func supplyChainInt(payload map[string]any, key string) int {
	value := payloadStr(payload, key)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func missingImpactEvidence(finding SupplyChainImpactFinding) []string {
	var missing []string
	if finding.PackageID == "" || finding.ObservedVersion == "" {
		missing = append(missing, "package version evidence missing")
	}
	if finding.RepositoryID == "" {
		missing = append(missing, "repository dependency evidence missing")
	}
	if finding.SubjectDigest == "" && finding.RuntimeReachability != "known_fixed" &&
		!supplyChainReachabilityHasPackageAnchor(finding.RuntimeReachability) {
		missing = append(missing, "image or SBOM attachment evidence missing")
	}
	if finding.RuntimeReachability != "known_fixed" && len(finding.Environments) == 0 {
		missing = append(missing, "deployment exposure evidence missing")
	}
	if finding.RuntimeReachability != "known_fixed" && len(finding.WorkloadIDs) == 0 {
		missing = append(missing, "workload evidence missing")
	}
	if finding.RuntimeReachability != "known_fixed" && len(finding.ServiceIDs) == 0 {
		missing = append(missing, "service evidence missing")
	}
	return uniqueSortedStrings(missing)
}

func supplyChainReachabilityHasPackageAnchor(runtimeReachability string) bool {
	switch runtimeReachability {
	case "package_manifest",
		jsTSPackageAPICallEvidence,
		jsTSPackageAPIImportEvidence,
		jsTSPackageAPIReExportEvidence,
		jsTSPackageAPISCIPCallEvidence,
		jsTSPackageAPIUnknownEvidence,
		jsTSPackageAPIAmbiguousEvidence,
		jsTSPackageAPIMissingEvidence:
		return true
	default:
		return false
	}
}
