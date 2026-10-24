// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// bindingYarnrc seeds every .yarnrc.yml credential shape the #8833 trigger
// list names: npmAuthToken and npmAuthIdent at top level and under npmScopes
// and npmRegistries, userinfo in the URL settings, and a Slack token in a
// comment.
const bindingYarnrc = `nodeLinker: node-modules
npmAuthToken: TokSeedAAAA11112222
npmAuthIdent: "IdentSeedBBBB33334444"
npmRegistryServer: "https://ci-user:RegPassSeedCCCC@npm.example.com/"
npmPublishRegistry: "${PUBLISH_REGISTRY}"
npmScopes:
  acme:
    npmAuthToken: ScopeSeedDDDD55556666
    npmRegistryServer: "https://acme.example.com"
npmRegistries:
  "//npm.internal.example.com":
    npmAuthIdent: RegIdentSeedEEEE7777
    npmAlwaysAuth: true
# old token xoxb-0123456789abSlackSeedFFFF
httpsProxy: "http://proxyuser:ProxySeedGGGG@proxy.example.com:3128"
`

var bindingYarnrcSeeds = []string{
	"TokSeedAAAA", "IdentSeedBBBB", "RegPassSeedCCCC", "ci-user", "ScopeSeedDDDD", "RegIdentSeedEEEE", "SlackSeedFFFF", "ProxySeedGGGG", "proxyuser",
}

const bindingTfvars = `db_password = "PwSeedHHHH11112222"
app_secrets = {
  stripe = "StripeSeedIIII3333"
}
user_data = <<EOT
export TOKEN=HeredocSeedJJJJ4444
EOT
# legacy token = CommentSeedKKKK5555
endpoint = "https://svc:UrlSeedLLLL6666@db.example.com/x"
instance_count = 3
`

var bindingTfvarsSeeds = []string{"PwSeedHHHH", "StripeSeedIIII", "HeredocSeedJJJJ", "CommentSeedKKKK", "UrlSeedLLLL", "svc:"}

const bindingTfvarsJSON = `{
  "db_password": "JsonPwSeedMMMM7777",
  "nested": {"token": "JsonTokSeedNNNN8888"},
  "endpoint": "https://u:JsonUrlSeedOOOO@h.example.com"
}
`

var bindingTfvarsJSONSeeds = []string{"JsonPwSeedMMMM", "JsonTokSeedNNNN", "JsonUrlSeedOOOO"}

// TestBoundCredentialFilesLeakNoSeedInAnyFact is the #8812 A1 binding
// precondition on #8833: for every path shape the tfvars and yarnrc bindings
// cover, the full fact stream (content, file, content_entity, and every other
// emitter) holds none of the seeded credentials, parser output included.
func TestBoundCredentialFilesLeakNoSeedInAnyFact(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path, body string
		seeds      []string
	}{
		{".yarnrc.yml", bindingYarnrc, bindingYarnrcSeeds},
		{"packages/web/.yarnrc.yml", bindingYarnrc, bindingYarnrcSeeds},
		{"terraform.tfvars", bindingTfvars, bindingTfvarsSeeds},
		{"env/prod.auto.tfvars", bindingTfvars, bindingTfvarsSeeds},
		{"terraform.tfvars.json", bindingTfvarsJSON, bindingTfvarsJSONSeeds},
	}
	for _, tc := range cases {
		_, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), tc.path, tc.body)
		for _, seed := range tc.seeds {
			if kinds := factKindsWithSeed(t, envelopes, seed); len(kinds) > 0 {
				t.Errorf("%s: seed %q reached fact kinds %v", tc.path, seed, kinds)
			}
		}
		if strings.HasSuffix(tc.path, ".tfvars.json") {
			// The hcl engine cannot parse a JSON body, before and after #8833
			// (TestDedicatedCredentialDefinitionsParseLikeTheirEngines pins the
			// equal error), so the collector stores no row for the path at all.
			// When the engine learns JSON, the skeleton policy already covers it.
			if hasFact(envelopes, "content", tc.path) || hasFact(envelopes, "file", tc.path) {
				t.Errorf("%s: a .tfvars.json body now emits facts; extend this test to check the skeleton body", tc.path)
			}
			continue
		}
		body := asString(contentFact(t, envelopes, tc.path).Payload["content_body"])
		if strings.Count(body, "\n") != strings.Count(tc.body, "\n") {
			t.Errorf("%s: stored body has %d lines, source %d", tc.path, strings.Count(body, "\n"), strings.Count(tc.body, "\n"))
		}
		if !hasFact(envelopes, "file", tc.path) {
			t.Errorf("%s: the file fact is gone; parser facts must survive the policy", tc.path)
		}
	}
}

// TestBoundYarnrcKeepsHostsAndReferences checks the .yarnrc.yml acceptance
// shapes that do not depend on the detector migration: hosts stay readable,
// an exact ${NAME} survives, a 5-character token and a misplaced AWS key under
// npmAuthToken are redacted, and the Slack comment keeps its kind marker.
func TestBoundYarnrcKeepsHostsAndReferences(t *testing.T) {
	t.Parallel()

	_, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), ".yarnrc.yml", bindingYarnrc+
		"unsafeHttpWhitelist:\n  - \"*.example.com\"\n")
	body := asString(contentFact(t, envelopes, ".yarnrc.yml").Payload["content_body"])
	for _, want := range []string{
		"@npm.example.com/\"", "@proxy.example.com:3128\"", "npmRegistryServer: \"https://acme.example.com\"",
		"npmPublishRegistry: \"${PUBLISH_REGISTRY}\"", "npmAuthToken: REDACTED_api_token", "# old token REDACTED_slack_token",
		"nodeLinker: node-modules", "\"*.example.com\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stored .yarnrc.yml lacks %q:\n%s", want, body)
		}
	}
	for name, src := range map[string]string{
		"five-character token": "npmAuthToken: abcde\n",
		"misplaced AWS key":    "npmAuthToken: AKIAABCDEFGHIJKLMNOP\n",
		"env reference":        "npmAuthToken: \"${NPM_TOKEN}\"\n",
	} {
		_, env := snapshotWithPolicy(t, parser.DefaultRegistry(), ".yarnrc.yml", src)
		got := asString(contentFact(t, env, ".yarnrc.yml").Payload["content_body"])
		want := map[string]string{
			"five-character token": "npmAuthToken: REDACTED_api_token\n",
			"misplaced AWS key":    "npmAuthToken: REDACTED_aws_access_key\n",
			"env reference":        src,
		}[name]
		if got != want {
			t.Errorf("%s: stored %q, want %q", name, got, want)
		}
	}
}

func hasFact(envelopes []facts.Envelope, kind, path string) bool {
	for _, envelope := range envelopes {
		if envelope.FactKind == kind && envelope.Payload["relative_path"] == path {
			return true
		}
	}
	return false
}
