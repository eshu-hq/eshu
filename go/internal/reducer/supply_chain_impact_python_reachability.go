// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	pythonReachabilityParserImport    = "python_parser_import"
	pythonReachabilityParserCall      = "python_parser_call"
	pythonReachabilityParserDecorator = "python_parser_decorator"
	pythonReachabilitySCIPCall        = "python_scip_call"
)

type pythonReachabilityRepositoryEvidence struct {
	importedModules  map[string][]string
	calledModules    map[string][]string
	decoratedModules map[string][]string
	scipSymbols      map[string][]string
	dynamicImport    bool
	pluginLoading    bool
}

type pythonReachabilityRepositoryScope struct {
	scopeID      string
	generationID string
}

func (h SupplyChainImpactHandler) loadPythonReachabilityEvidenceFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	if !supplyChainImpactHasPyPIEvidence(envelopes) {
		return nil, nil
	}
	repositoryIDs := supplyChainImpactFilter(envelopes).RepositoryIDs
	if len(repositoryIDs) == 0 {
		return nil, nil
	}
	repositoriesByScope := pythonReachabilityRepositoryIDsByScope(envelopes, repositoryIDs)
	if len(repositoriesByScope) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	for _, repoScope := range sortedPythonReachabilityRepositoryScopes(repositoriesByScope) {
		fileFacts, err := loadFactsForKindAndPayloadValue(
			ctx,
			h.FactLoader,
			repoScope.scopeID,
			repoScope.generationID,
			factKindFile,
			"repo_id",
			repositoriesByScope[repoScope],
		)
		if err != nil {
			return nil, err
		}
		loaded = appendUniqueSupplyChainImpactFacts(loaded, fileFacts...)
	}
	return loaded, nil
}

func pythonReachabilityRepositoryIDsByScope(
	envelopes []facts.Envelope,
	repositoryIDs []string,
) map[pythonReachabilityRepositoryScope][]string {
	needed := make(map[string]struct{}, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		needed[repositoryID] = struct{}{}
	}
	if len(needed) == 0 {
		return nil
	}

	out := map[pythonReachabilityRepositoryScope][]string{}
	for _, envelope := range envelopes {
		if envelope.FactKind != factKindRepository || envelope.IsTombstone {
			continue
		}
		repositoryID := firstNonBlank(
			payloadStr(envelope.Payload, "graph_id"),
			payloadStr(envelope.Payload, "repo_id"),
			payloadStr(envelope.Payload, "repository_id"),
			packageSourceRepositoryIDFromScope(envelope.ScopeID),
		)
		if _, ok := needed[repositoryID]; !ok {
			continue
		}
		repoScope := pythonReachabilityRepositoryScope{
			scopeID:      strings.TrimSpace(envelope.ScopeID),
			generationID: strings.TrimSpace(envelope.GenerationID),
		}
		if repoScope.scopeID == "" || repoScope.generationID == "" {
			continue
		}
		out[repoScope] = append(out[repoScope], repositoryID)
	}
	for repoScope, ids := range out {
		out[repoScope] = uniqueSortedStrings(ids)
	}
	return out
}

func sortedPythonReachabilityRepositoryScopes(
	repositoriesByScope map[pythonReachabilityRepositoryScope][]string,
) []pythonReachabilityRepositoryScope {
	repoScopes := make([]pythonReachabilityRepositoryScope, 0, len(repositoriesByScope))
	for repoScope := range repositoriesByScope {
		repoScopes = append(repoScopes, repoScope)
	}
	sort.Slice(repoScopes, func(i, j int) bool {
		if repoScopes[i].scopeID != repoScopes[j].scopeID {
			return repoScopes[i].scopeID < repoScopes[j].scopeID
		}
		return repoScopes[i].generationID < repoScopes[j].generationID
	})
	return repoScopes
}

func supplyChainImpactHasPyPIEvidence(envelopes []facts.Envelope) bool {
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		switch envelope.FactKind {
		case facts.VulnerabilityAffectedPackageFactKind,
			facts.PackageRegistryPackageFactKind,
			packageConsumptionCorrelationFactKind:
			if normalizedSupplyChainVersionEcosystem(payloadStr(envelope.Payload, "ecosystem")) == "pypi" {
				return true
			}
			if strings.HasPrefix(strings.ToLower(payloadStr(envelope.Payload, "package_id")), "pkg:pypi/") {
				return true
			}
		}
	}
	return false
}

func applyPythonSupplyChainReachability(
	finding *SupplyChainImpactFinding,
	pkgs []supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) []string {
	if normalizedSupplyChainVersionEcosystem(finding.Ecosystem) != "pypi" {
		return nil
	}
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	if repositoryID == "" {
		return []string{"python parser repository scope missing"}
	}
	evidence, ok := index.pythonReachability[repositoryID]
	if !ok || !evidence.hasParserEvidence() {
		return []string{"python parser or SCIP reachability evidence missing"}
	}
	identities := pythonPackageAPIIdentities(representativeAffectedPackage(pkgs))
	if len(identities) == 0 {
		return evidence.ambiguousMissingEvidence("python package API identity missing")
	}
	for _, identity := range identities {
		if factIDs := evidence.callFactIDs(identity); len(factIDs) > 0 {
			finding.RuntimeReachability = pythonReachabilityParserCall
			appendPythonReachabilityEvidence(finding, factIDs)
			return nil
		}
		if factIDs := evidence.scipCallFactIDs(identity); len(factIDs) > 0 {
			finding.RuntimeReachability = pythonReachabilitySCIPCall
			appendPythonReachabilityEvidence(finding, factIDs)
			return nil
		}
		if factIDs := evidence.decoratorFactIDs(identity); len(factIDs) > 0 {
			finding.RuntimeReachability = pythonReachabilityParserDecorator
			appendPythonReachabilityEvidence(finding, factIDs)
			return nil
		}
		if factIDs := evidence.importFactIDs(identity); len(factIDs) > 0 {
			finding.RuntimeReachability = pythonReachabilityParserImport
			appendPythonReachabilityEvidence(finding, factIDs)
			return nil
		}
	}
	return evidence.ambiguousMissingEvidence("python package API evidence missing")
}

func appendPythonReachabilityEvidence(
	finding *SupplyChainImpactFinding,
	fileFactIDs []string,
) {
	finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, fileFactIDs...)
	finding.EvidencePath = append(finding.EvidencePath, factKindFile)
	finding.EvidenceFactIDs = uniqueSortedStrings(finding.EvidenceFactIDs)
	finding.EvidencePath = uniqueSortedStrings(finding.EvidencePath)
}

func (e pythonReachabilityRepositoryEvidence) hasParserEvidence() bool {
	return len(e.importedModules) > 0 ||
		len(e.calledModules) > 0 ||
		len(e.decoratedModules) > 0 ||
		len(e.scipSymbols) > 0 ||
		e.dynamicImport ||
		e.pluginLoading
}

func (e pythonReachabilityRepositoryEvidence) importFactIDs(identity string) []string {
	return uniqueSortedStrings(e.importedModules[identity])
}

func (e pythonReachabilityRepositoryEvidence) callFactIDs(identity string) []string {
	return uniqueSortedStrings(e.calledModules[identity])
}

func (e pythonReachabilityRepositoryEvidence) decoratorFactIDs(identity string) []string {
	return uniqueSortedStrings(e.decoratedModules[identity])
}

func (e pythonReachabilityRepositoryEvidence) scipCallFactIDs(identity string) []string {
	var factIDs []string
	for symbol, symbolFactIDs := range e.scipSymbols {
		if pythonSCIPSymbolMatchesPackage(symbol, identity) {
			factIDs = append(factIDs, symbolFactIDs...)
		}
	}
	return uniqueSortedStrings(factIDs)
}

func (e pythonReachabilityRepositoryEvidence) ambiguousMissingEvidence(reason string) []string {
	missing := []string{reason}
	if e.dynamicImport {
		missing = append(missing, "python dynamic import evidence ambiguous")
	}
	if e.pluginLoading {
		missing = append(missing, "python plugin loading evidence ambiguous")
	}
	return uniqueSortedStrings(missing)
}

func pythonPackageAPIIdentities(pkg supplyChainAffectedPackage) []string {
	candidates := []string{
		strings.TrimSpace(pkg.name),
		pythonPackageNameFromPURL(pkg.purl),
		pythonPackageNameFromPackageID(pkg.packageID),
	}
	identities := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		identity := pythonImportIdentity(candidate)
		if identity == "" {
			continue
		}
		identities = append(identities, identity)
	}
	return uniqueSortedStrings(identities)
}

func pythonPackageNameFromPackageID(packageID string) string {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(packageID), "pkg:pypi/") {
		return strings.TrimPrefix(packageID, "pkg:pypi/")
	}
	if idx := strings.LastIndex(packageID, "/"); idx >= 0 && idx+1 < len(packageID) {
		return packageID[idx+1:]
	}
	return ""
}

func pythonPackageNameFromPURL(purl string) string {
	purl = strings.TrimSpace(purl)
	if purl == "" {
		return ""
	}
	if at := strings.Index(purl, "@"); at >= 0 {
		purl = purl[:at]
	}
	if idx := strings.LastIndex(purl, "/"); idx >= 0 && idx+1 < len(purl) {
		return purl[idx+1:]
	}
	return ""
}

func pythonImportIdentity(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "-./") {
		return ""
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return ""
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ""
		}
	}
	return strings.ToLower(name)
}

func pythonImportRoot(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	if value == "" || strings.HasPrefix(value, ".") {
		return ""
	}
	value = strings.Trim(value, "'\"`")
	if idx := strings.IndexAny(value, "(.["); idx >= 0 {
		value = value[:idx]
	}
	return pythonImportIdentity(value)
}

func pythonSCIPSymbolMatchesPackage(symbol string, identity string) bool {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	identity = strings.ToLower(strings.TrimSpace(identity))
	if symbol == "" || identity == "" {
		return false
	}
	for _, token := range strings.FieldsFunc(symbol, func(r rune) bool {
		return r == ' ' || r == '/' || r == '#' || r == '.' || r == ':' || r == '(' || r == ')'
	}) {
		if token == identity {
			return true
		}
	}
	return false
}

func extractPythonReachabilityEvidence(envelopes []facts.Envelope) map[string]pythonReachabilityRepositoryEvidence {
	out := make(map[string]pythonReachabilityRepositoryEvidence)
	for _, envelope := range envelopes {
		if envelope.FactKind != factKindFile || envelope.IsTombstone {
			continue
		}
		repositoryID := payloadStr(envelope.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		fileData := payloadMap(envelope.Payload, "parsed_file_data")
		if fileData == nil {
			continue
		}
		evidence := out[repositoryID]
		evidence.ensureMaps()
		aliases := collectPythonImportEvidence(&evidence, envelope.FactID, fileData)
		collectPythonCallEvidence(&evidence, envelope.FactID, fileData, aliases)
		collectPythonDecoratorEvidence(&evidence, envelope.FactID, fileData, aliases)
		for _, edge := range mapSlice(fileData["function_calls_scip"]) {
			if symbol := strings.TrimSpace(payloadStr(edge, "callee_symbol")); symbol != "" {
				evidence.scipSymbols[symbol] = append(evidence.scipSymbols[symbol], envelope.FactID)
			}
		}
		out[repositoryID] = evidence
	}
	return out
}

func (e *pythonReachabilityRepositoryEvidence) ensureMaps() {
	if e.importedModules == nil {
		e.importedModules = map[string][]string{}
	}
	if e.calledModules == nil {
		e.calledModules = map[string][]string{}
	}
	if e.decoratedModules == nil {
		e.decoratedModules = map[string][]string{}
	}
	if e.scipSymbols == nil {
		e.scipSymbols = map[string][]string{}
	}
}

func collectPythonImportEvidence(
	evidence *pythonReachabilityRepositoryEvidence,
	factID string,
	fileData map[string]any,
) map[string]string {
	aliases := make(map[string]string)
	for _, entry := range mapSlice(fileData["imports"]) {
		if lang := strings.ToLower(strings.TrimSpace(payloadStr(entry, "lang"))); lang != "" && lang != "python" {
			continue
		}
		root := pythonImportRoot(firstNonBlank(payloadStr(entry, "source"), payloadStr(entry, "name")))
		if root == "" {
			continue
		}
		evidence.importedModules[root] = append(evidence.importedModules[root], factID)
		for _, alias := range []string{
			payloadStr(entry, "alias"),
			payloadStr(entry, "name"),
		} {
			if aliasRoot := pythonImportRoot(alias); aliasRoot != "" {
				aliases[aliasRoot] = root
			}
		}
	}
	return aliases
}

func collectPythonCallEvidence(
	evidence *pythonReachabilityRepositoryEvidence,
	factID string,
	fileData map[string]any,
	aliases map[string]string,
) {
	for _, call := range mapSlice(fileData["function_calls"]) {
		if lang := strings.ToLower(strings.TrimSpace(payloadStr(call, "lang"))); lang != "" && lang != "python" {
			continue
		}
		fullName := firstNonBlank(payloadStr(call, "full_name"), payloadStr(call, "name"))
		if pythonDynamicImportCall(fullName) {
			evidence.dynamicImport = true
		}
		if pythonPluginLoadingCall(fullName) {
			evidence.pluginLoading = true
		}
		root := pythonImportRoot(fullName)
		if root == "" {
			continue
		}
		if mapped := aliases[root]; mapped != "" {
			root = mapped
		}
		evidence.calledModules[root] = append(evidence.calledModules[root], factID)
	}
}

func collectPythonDecoratorEvidence(
	evidence *pythonReachabilityRepositoryEvidence,
	factID string,
	fileData map[string]any,
	aliases map[string]string,
) {
	for _, bucket := range []string{"functions", "classes"} {
		for _, entity := range mapSlice(fileData[bucket]) {
			for _, decorator := range semanticPayloadStringSlice(entity, "decorators") {
				root := pythonImportRoot(decorator)
				if root == "" {
					continue
				}
				if mapped := aliases[root]; mapped != "" {
					root = mapped
				}
				evidence.decoratedModules[root] = append(evidence.decoratedModules[root], factID)
			}
		}
	}
}

func pythonDynamicImportCall(fullName string) bool {
	fullName = strings.ToLower(strings.TrimSpace(fullName))
	return fullName == "__import__" ||
		fullName == "importlib.import_module" ||
		strings.HasSuffix(fullName, ".import_module")
}

func pythonPluginLoadingCall(fullName string) bool {
	fullName = strings.ToLower(strings.TrimSpace(fullName))
	return fullName == "pkg_resources.iter_entry_points" ||
		fullName == "importlib.metadata.entry_points" ||
		strings.HasSuffix(fullName, ".entry_points")
}
