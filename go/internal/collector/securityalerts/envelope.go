package securityalerts

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

const githubDependabotProvider = "github_dependabot"

// NewGitHubDependabotAlertEnvelope converts one GitHub Dependabot alert into a
// repository-scoped provider security-alert source fact.
//
// When ctx.RepositoryID is non-empty (org-wide targets), it is used for
// payload["repository_id"] and the stableFactKey so reducer reconciliation
// keys on the per-repository scope. ctx.ScopeID (the org generation scope) is
// used for env.ScopeID so the envelope matches the committed generation scope
// required by the Postgres streaming fact writer.
func NewGitHubDependabotAlertEnvelope(
	ctx EnvelopeContext,
	alert GitHubDependabotAlert,
) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	if alert.Number <= 0 {
		return facts.Envelope{}, fmt.Errorf("github dependabot alert number must be positive")
	}
	identity, err := packageIdentityFromDependabot(alert)
	if err != nil {
		return facts.Envelope{}, err
	}
	// repositoryID is the per-repository scope used for reducer keying and
	// stable fact dedup. For per-repository targets it equals ctx.ScopeID.
	// For org targets ctx.RepositoryID carries the per-repo scope while
	// ctx.ScopeID holds the org generation scope that the Postgres streaming
	// writer requires on every envelope (envelope.ScopeID == committed scope).
	repositoryID := ctx.ScopeID
	if r := strings.TrimSpace(ctx.RepositoryID); r != "" {
		repositoryID = r
	}
	providerAlertID := githubDependabotAlertID(repositoryID, alert.Number)
	stableFactKey := facts.StableID(facts.SecurityAlertRepositoryAlertFactKind, map[string]any{
		"provider":              githubDependabotProvider,
		"provider_alert_number": alert.Number,
		"repository_id":         repositoryID,
	})
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              githubDependabotProvider,
		"provider_alert_id":     providerAlertID,
		"provider_alert_number": int64(alert.Number),
		"provider_state":        strings.TrimSpace(alert.State),
		"repository_id":         repositoryID,
		"ecosystem":             string(identity.Ecosystem),
		"package_name":          identity.NormalizedName,
		"package_id":            identity.PackageID,
		"manifest_path":         strings.TrimSpace(alert.Dependency.ManifestPath),
		"dependency_scope":      strings.TrimSpace(alert.Dependency.Scope),
		"relationship":          strings.TrimSpace(alert.Dependency.Relationship),
		"ghsa_ids":              githubDependabotGHSAIDs(alert.SecurityAdvisory),
		"cve_ids":               githubDependabotCVEIDs(alert.SecurityAdvisory),
		"vulnerable_range":      strings.TrimSpace(alert.SecurityVulnerability.VulnerableVersionRange),
		"patched_version":       strings.TrimSpace(alert.SecurityVulnerability.FirstPatchedVersion.Identifier),
		"severity":              strings.ToLower(strings.TrimSpace(alert.SecurityAdvisory.Severity)),
		"cvss":                  githubDependabotCVSSPayload(alert.SecurityAdvisory.CVSS),
		"epss":                  githubDependabotEPSSPayload(alert.SecurityAdvisory.EPSS),
		"cwes":                  githubDependabotCWEPayload(alert.SecurityAdvisory.CWEs),
		"summary":               strings.TrimSpace(alert.SecurityAdvisory.Summary),
		"description":           strings.TrimSpace(alert.SecurityAdvisory.Description),
		"created_at":            strings.TrimSpace(alert.CreatedAt),
		"updated_at":            strings.TrimSpace(alert.UpdatedAt),
		"fixed_at":              strings.TrimSpace(alert.FixedAt),
		"dismissed_at":          strings.TrimSpace(alert.DismissedAt),
		"source_url":            sanitizeURL(alert.HTMLURL),
		"correlation_anchors":   githubDependabotCorrelationAnchors(providerAlertID, identity.PackageID, alert),
	}
	return facts.Envelope{
		FactID:           securityAlertFactID(stableFactKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableFactKey,
			SourceURI:      sanitizeURL(ctx.SourceURI),
			SourceRecordID: providerAlertID,
		},
	}, nil
}

func validateEnvelopeContext(ctx EnvelopeContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("security alert envelope scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("security alert envelope generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("security alert envelope collector_instance_id must not be blank")
	}
	return nil
}

func packageIdentityFromDependabot(alert GitHubDependabotAlert) (packageidentity.Identity, error) {
	pkg := alert.Dependency.Package
	if strings.TrimSpace(pkg.Name) == "" {
		pkg = alert.SecurityVulnerability.Package
	}
	ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(pkg.Ecosystem))
	if ecosystem == "" {
		return packageidentity.Identity{}, fmt.Errorf("github dependabot package ecosystem %q is unsupported", pkg.Ecosystem)
	}
	name := strings.TrimSpace(pkg.Name)
	namespace := ""
	if ecosystem == packageidentity.EcosystemMaven && strings.Contains(name, ":") {
		namespace, name, _ = strings.Cut(name, ":")
	}
	identity, err := packageidentity.Normalize(packageidentity.RawIdentity{
		Ecosystem: ecosystem,
		Registry:  defaultRegistryForEcosystem(ecosystem),
		RawName:   name,
		Namespace: namespace,
	})
	if err != nil {
		return packageidentity.Identity{}, fmt.Errorf("normalize github dependabot package: %w", err)
	}
	return identity, nil
}

func defaultRegistryForEcosystem(ecosystem packageidentity.Ecosystem) string {
	switch ecosystem {
	case packageidentity.EcosystemNPM:
		return "registry.npmjs.org"
	case packageidentity.EcosystemPyPI:
		return "pypi.org/simple"
	case packageidentity.EcosystemGoModule:
		return "proxy.golang.org"
	case packageidentity.EcosystemMaven:
		return "repo.maven.apache.org/maven2"
	case packageidentity.EcosystemNuGet:
		return "api.nuget.org/v3/index.json"
	case packageidentity.EcosystemComposer:
		return "repo.packagist.org"
	case packageidentity.EcosystemRubyGems:
		return "rubygems.org"
	case packageidentity.EcosystemCargo:
		return "crates.io"
	default:
		return "unknown"
	}
}

func githubDependabotAlertID(scopeID string, number int) string {
	return strings.Join([]string{
		githubDependabotProvider,
		strings.TrimSpace(scopeID),
		strconv.Itoa(number),
	}, ":")
}

func securityAlertFactID(stableFactKey string, scopeID string, generationID string) string {
	return facts.StableID("SecurityAlertFact", map[string]any{
		"fact_kind":       facts.SecurityAlertRepositoryAlertFactKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func githubDependabotGHSAIDs(advisory GitHubDependabotSecurityAdvisory) []string {
	ids := []string{advisory.GHSAID}
	for _, identifier := range advisory.Identifiers {
		if strings.EqualFold(identifier.Type, "GHSA") {
			ids = append(ids, identifier.Value)
		}
	}
	return cleanStrings(ids)
}

func githubDependabotCVEIDs(advisory GitHubDependabotSecurityAdvisory) []string {
	ids := []string{advisory.CVEID}
	for _, identifier := range advisory.Identifiers {
		if strings.EqualFold(identifier.Type, "CVE") {
			ids = append(ids, identifier.Value)
		}
	}
	return cleanStrings(ids)
}

func githubDependabotCVSSPayload(cvss GitHubDependabotCVSS) map[string]any {
	payload := map[string]any{}
	if cvss.Score != 0 {
		payload["score"] = cvss.Score
	}
	if vector := strings.TrimSpace(cvss.Vector); vector != "" {
		payload["vector"] = vector
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func githubDependabotEPSSPayload(epss GitHubDependabotEPSS) map[string]string {
	payload := map[string]string{}
	if percentage := anyString(epss.Percentage); percentage != "" {
		payload["percentage"] = percentage
	}
	if percentile := anyString(epss.Percentile); percentile != "" {
		payload["percentile"] = percentile
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func githubDependabotCWEPayload(cwes []GitHubDependabotCWE) []map[string]string {
	out := make([]map[string]string, 0, len(cwes))
	seen := make(map[string]struct{}, len(cwes))
	for _, cwe := range cwes {
		cweID := strings.TrimSpace(cwe.CWEID)
		if cweID == "" {
			continue
		}
		if _, ok := seen[cweID]; ok {
			continue
		}
		seen[cweID] = struct{}{}
		out = append(out, map[string]string{
			"cwe_id": cweID,
			"name":   strings.TrimSpace(cwe.Name),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func githubDependabotCorrelationAnchors(
	providerAlertID string,
	packageID string,
	alert GitHubDependabotAlert,
) []string {
	anchors := []string{providerAlertID, packageID}
	anchors = append(anchors, githubDependabotGHSAIDs(alert.SecurityAdvisory)...)
	anchors = append(anchors, githubDependabotCVEIDs(alert.SecurityAdvisory)...)
	return cleanStrings(anchors)
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}

func anyString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}

func sanitizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if isSensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "api_key", "apikey", "auth", "authorization", "jwt",
		"key", "password", "passwd", "secret", "sig", "signature", "token",
		"x-amz-credential", "x-amz-security-token", "x-amz-signature":
		return true
	default:
		return false
	}
}
