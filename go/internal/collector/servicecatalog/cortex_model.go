package servicecatalog

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// cortexDescriptor is the typed projection of one cortex.yaml document. A Cortex
// entity descriptor is a fully compliant OpenAPI 3 spec with Cortex-specific
// `x-cortex-*` extensions under `info`. Only the fields the producer maps into
// facts are modeled.
type cortexDescriptor struct {
	OpenAPI string           `yaml:"openapi"`
	Info    cortexEntityInfo `yaml:"info"`
}

// cortexEntityInfo holds the OpenAPI `info` block plus the Cortex extensions the
// producer reads.
type cortexEntityInfo struct {
	Title        string         `yaml:"title"`
	Description  string         `yaml:"description"`
	Tag          string         `yaml:"x-cortex-tag"`
	Type         string         `yaml:"x-cortex-type"`
	Groups       []string       `yaml:"x-cortex-groups"`
	Owners       []cortexOwner  `yaml:"x-cortex-owners"`
	Git          cortexGit      `yaml:"x-cortex-git"`
	Links        []cortexLink   `yaml:"x-cortex-link"`
	Dependencies []cortexDepend `yaml:"x-cortex-dependency"`
}

// cortexOwner models one x-cortex-owners entry. Cortex owners carry a type
// (group or email), a name, and a provider (CORTEX, GITHUB, GITLAB, ...).
type cortexOwner struct {
	Type     string `yaml:"type"`
	Name     string `yaml:"name"`
	Provider string `yaml:"provider"`
}

// cortexLink models one x-cortex-link entry: an operational link with a name, a
// type (RUNBOOK, DASHBOARD, DOCUMENTATION, ...), and a URL.
type cortexLink struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
}

// cortexDepend models one x-cortex-dependency entry. Cortex identifies the
// target by its tag.
type cortexDepend struct {
	Tag string `yaml:"tag"`
}

// cortexGit models the x-cortex-git block. The provider key (github, gitlab,
// bitbucket, azure, ...) is dynamic, so it is decoded into a map keyed by the
// lowercased provider name.
type cortexGit struct {
	Providers map[string]cortexGitProvider
}

// cortexGitProvider models one provider entry under x-cortex-git. GitHub,
// GitLab, and Bitbucket carry a `repository` slug; Azure DevOps splits it into
// `project` plus `repository`. `basepath` is accepted but unused (it locates a
// service within a monorepo, not the repository itself).
type cortexGitProvider struct {
	Repository string `yaml:"repository"`
	Project    string `yaml:"project"`
	Basepath   string `yaml:"basepath"`
}

// UnmarshalYAML decodes the dynamic provider keys under x-cortex-git into the
// Providers map, lowercasing each provider name so host derivation is
// case-insensitive.
func (g *cortexGit) UnmarshalYAML(value *yaml.Node) error {
	raw := map[string]cortexGitProvider{}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	g.Providers = make(map[string]cortexGitProvider, len(raw))
	for provider, entry := range raw {
		g.Providers[strings.ToLower(strings.TrimSpace(provider))] = entry
	}
	return nil
}

// supportedCortexOpenAPIVersions enumerates the OpenAPI descriptor versions the
// producer fully understands. Other versions still emit a minimally-parsed
// entity plus an unsupported_descriptor_version warning rather than a silent
// drop.
var supportedCortexOpenAPIVersions = map[string]bool{
	"3.0.0": true,
	"3.0.1": true,
	"3.0.2": true,
	"3.0.3": true,
	"3.1.0": true,
}

// cortexDefaultType is the entity kind used when a descriptor omits an explicit
// x-cortex-type. Cortex entities without a type are treated as services.
const cortexDefaultType = "service"

// cortexProviderHosts maps known public git providers to their canonical host.
// Expanding only these hosts keeps the producer from guessing a host for an
// unknown or self-hosted provider, which would manufacture a wrong derivation
// and risk a false correlation.
var cortexProviderHosts = map[string]string{
	"github":    "https://github.com",
	"gitlab":    "https://gitlab.com",
	"bitbucket": "https://bitbucket.org",
}

// version returns the trimmed OpenAPI descriptor version.
func (d cortexDescriptor) version() string {
	return strings.TrimSpace(d.OpenAPI)
}

// entityRef returns the canonical `type:cortex/tag` reference for one Cortex
// entity, or an empty string when the tag is missing. The x-cortex-tag is the
// globally unique anchor; it is namespaced under `cortex` so refs stay distinct
// from other providers' refs in the shared reducer entity key.
func (i cortexEntityInfo) entityRef() string {
	tag := strings.ToLower(strings.TrimSpace(i.Tag))
	if tag == "" {
		return ""
	}
	kind := strings.ToLower(strings.TrimSpace(i.Type))
	if kind == "" {
		kind = cortexDefaultType
	}
	return kind + ":" + ProviderCortexNamespace + "/" + tag
}

// entityType returns the declared x-cortex-type, defaulting to service.
func (i cortexEntityInfo) entityType() string {
	t := strings.TrimSpace(i.Type)
	if t == "" {
		return cortexDefaultType
	}
	return t
}

// ownerRef returns the first declared owner name verbatim. Cortex owners carry
// a provider and type, but the reducer records the declared owner string as
// provenance only; the name is preserved as-is so distinct owners are not
// merged. An empty name is skipped in favor of the first non-blank owner.
func (i cortexEntityInfo) ownerRef() string {
	for _, owner := range i.Owners {
		name := strings.TrimSpace(owner.Name)
		if name != "" {
			return name
		}
	}
	return ""
}

// tier extracts a tier label from `tier-N` groups, matching common Cortex group
// conventions. It returns the first matching group's full label.
func (i cortexEntityInfo) tier() string {
	for _, group := range i.Groups {
		group = strings.TrimSpace(group)
		if strings.HasPrefix(group, "tier-") {
			return group
		}
	}
	return ""
}

// dependencies returns the declared dependency tags.
func (i cortexEntityInfo) dependencies() []string {
	out := make([]string, 0, len(i.Dependencies))
	for _, dep := range i.Dependencies {
		tag := strings.TrimSpace(dep.Tag)
		if tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// repositoryLocator resolves the x-cortex-git block into a declared repository
// URL for a known public provider, or a name-only slug otherwise. It returns
// (url, "") when a known host plus a path-shaped slug can be expanded, and
// ("", name) when the provider is unknown/self-hosted or the slug is not a path.
// It never fabricates a repository id.
//
// Provider keys are visited in sorted order so the result is deterministic when
// a descriptor declares more than one git provider; a non-deterministic map
// iteration would break the stable-fact-id idempotency contract.
func (g cortexGit) repositoryLocator() (repositoryURL, repositoryName string) {
	providers := make([]string, 0, len(g.Providers))
	for provider := range g.Providers {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		entry := g.Providers[provider]
		repo := strings.Trim(strings.TrimSpace(entry.Repository), "/")
		if repo == "" {
			continue
		}
		if provider == "azure" {
			return azureRepositoryURL(entry, repo)
		}
		host, known := cortexProviderHosts[provider]
		if !known {
			// Unknown or self-hosted provider: a host guess would be wrong, so
			// keep the slug as a name-only locator the reducer rejects.
			return "", repo
		}
		if !strings.Contains(repo, "/") {
			// A bare slug with no namespace path cannot form a valid repo URL.
			return "", repo
		}
		return host + "/" + repo, ""
	}
	return "", ""
}

// azureRepositoryURL expands an Azure DevOps entry. Azure splits the locator
// into a project and a repository, forming `https://dev.azure.com/{project}/
// _git/{repository}`. A missing project leaves a name-only locator.
func azureRepositoryURL(entry cortexGitProvider, repo string) (repositoryURL, repositoryName string) {
	project := strings.Trim(strings.TrimSpace(entry.Project), "/")
	if project == "" {
		return "", repo
	}
	return "https://dev.azure.com/" + project + "/_git/" + repo, ""
}
