package securityalerts

import "time"

// CollectorKind is the durable collector family name for provider security
// alert facts.
const CollectorKind = "security_alert"

// EnvelopeContext carries Eshu fact boundary fields for one provider security
// alert observation.
//
// ScopeID is the committed generation scope that the envelope belongs to. For
// per-repository targets it is the repository's canonical security-alert scope
// (security-alert:github:<owner>/<repo>). For organization-wide targets it is
// the org target scope (security-alert:github-org:<org>); RepositoryID carries
// the per-repository scope used for reducer keying and dedup.
//
// RepositoryID, when non-empty, overrides the repository_id payload field and
// the stableFactKey repository_id used for dedup. Leave it empty for
// per-repository targets — the envelope builder falls back to ScopeID.
type EnvelopeContext struct {
	ScopeID             string
	RepositoryID        string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// GitHubDependabotAlert is the GitHub Dependabot alert API subset normalized
// by Eshu.
type GitHubDependabotAlert struct {
	Number                int                                   `json:"number"`
	State                 string                                `json:"state"`
	Dependency            GitHubDependabotDependency            `json:"dependency"`
	SecurityAdvisory      GitHubDependabotSecurityAdvisory      `json:"security_advisory"`
	SecurityVulnerability GitHubDependabotSecurityVulnerability `json:"security_vulnerability"`
	HTMLURL               string                                `json:"html_url"`
	CreatedAt             string                                `json:"created_at"`
	UpdatedAt             string                                `json:"updated_at"`
	FixedAt               string                                `json:"fixed_at"`
	DismissedAt           string                                `json:"dismissed_at"`
	Repository            GitHubDependabotRepository            `json:"repository"`
}

// GitHubDependabotRepository identifies the repository an alert belongs to. It
// is populated by the organization-wide alerts endpoint
// (GET /orgs/{org}/dependabot/alerts) so a single org request can fan out into
// per-repository facts. The per-repository endpoint omits it because the
// repository is encoded in the request path.
type GitHubDependabotRepository struct {
	FullName string                          `json:"full_name"`
	Name     string                          `json:"name"`
	Owner    GitHubDependabotRepositoryOwner `json:"owner"`
}

// GitHubDependabotRepositoryOwner is the owner login embedded in an
// organization alert's repository object.
type GitHubDependabotRepositoryOwner struct {
	Login string `json:"login"`
}

// GitHubDependabotDependency identifies the repository-local dependency that
// triggered a Dependabot alert.
type GitHubDependabotDependency struct {
	Package      GitHubDependabotPackage `json:"package"`
	ManifestPath string                  `json:"manifest_path"`
	Scope        string                  `json:"scope"`
	Relationship string                  `json:"relationship"`
}

// GitHubDependabotPackage identifies one provider-reported package.
type GitHubDependabotPackage struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
}

// GitHubDependabotSecurityAdvisory is the advisory summary embedded in a
// Dependabot alert.
type GitHubDependabotSecurityAdvisory struct {
	GHSAID      string                       `json:"ghsa_id"`
	CVEID       string                       `json:"cve_id"`
	Identifiers []GitHubDependabotIdentifier `json:"identifiers"`
	Summary     string                       `json:"summary"`
	Description string                       `json:"description"`
	Severity    string                       `json:"severity"`
	CVSS        GitHubDependabotCVSS         `json:"cvss"`
	EPSS        GitHubDependabotEPSS         `json:"epss"`
	CWEs        []GitHubDependabotCWE        `json:"cwes"`
}

// GitHubDependabotIdentifier is one advisory identifier such as GHSA or CVE.
type GitHubDependabotIdentifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// GitHubDependabotCVSS is the source-reported CVSS summary.
type GitHubDependabotCVSS struct {
	Score  float64 `json:"score"`
	Vector string  `json:"vector_string"`
}

// GitHubDependabotEPSS is the source-reported EPSS summary.
type GitHubDependabotEPSS struct {
	Percentage any `json:"percentage"`
	Percentile any `json:"percentile"`
}

// GitHubDependabotCWE is one source-reported CWE summary.
type GitHubDependabotCWE struct {
	CWEID string `json:"cwe_id"`
	Name  string `json:"name"`
}

// GitHubDependabotSecurityVulnerability carries affected range and fixed
// version detail for one alert.
type GitHubDependabotSecurityVulnerability struct {
	Package                GitHubDependabotPackage `json:"package"`
	VulnerableVersionRange string                  `json:"vulnerable_version_range"`
	FirstPatchedVersion    GitHubDependabotVersion `json:"first_patched_version"`
}

// GitHubDependabotVersion identifies the first provider-reported patched
// version when one is available.
type GitHubDependabotVersion struct {
	Identifier string `json:"identifier"`
}
