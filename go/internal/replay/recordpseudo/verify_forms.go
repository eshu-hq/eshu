// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"regexp"
	"strings"
)

// The allow forms below mirror scripts/lib/cassette_private_data_pattern.sh
// one for one. When the gate's allowlist changes, change these with it; the
// recordpseudo package test compares Verify with the gate on the committed
// corpus so a drift is caught there.

// hostnameTLDs is the gate's TLD list verbatim.
const hostnameTLDs = "com|net|org|io|dev|app|cloud|co|ai|us|internal|local|svc|example|test|invalid|localhost" +
	"|corp|lan|home|intranet|private|edu|gov|mil|int|aws|consul|arpa" +
	"|info|biz|me|xyz|tech|online|site|store|shop|blog|live|news|pro|mobi|tv|ws|work|world|today|space|website|digital|network|systems|solutions|services|software|engineering|tools|team|group|company|global|host|hosting|page|zone|club|link|click|top|vip|fun|life|gg" +
	"|uk|de|ca|jp|au|nl|fr|eu|ch|se|dk|be|fi|ie|pt|br|mx|ar|cn|kr|sg|hk|tw|nz|za|ru|ua|il|ae|sa|tr|gr|hu|ro|bg|sk|si|hr|lt|lv|ee|lu|cz"

var (
	// The right boundary is INSIDE the pattern: Go's RE2 has no lookahead,
	// and with the boundary checked afterwards the TLD alternation settles on
	// "co" for "corp" and the candidate is lost. With the boundary in the
	// pattern the engine backtracks over the alternation as the gate's PCRE
	// does. The trailing boundary byte is trimmed by scanHostnames.
	hostnameCand    = regexp.MustCompile(`(?i)[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\.(?:` + hostnameTLDs + `)(?:[^a-z0-9_-]|\z)`)
	docAccountRe    = regexp.MustCompile(`^(?:12345678901[2]|0{11}[0-9])$`)
	pseudoAccountRe = regexp.MustCompile(`^0000[0-9]{8}$`)
	ipv4AllowRe     = regexp.MustCompile(`^(?:0\.0\.0\.0|192\.0\.2\.[0-9]{1,3}|198\.51\.100\.[0-9]{1,3}|203\.0\.113\.[0-9]{1,3}|127\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})$`)
	nodeIPAllowRe   = regexp.MustCompile(`^ip-(?:192-0-2-[0-9]{1,3}|198-51-100-[0-9]{1,3}|203-0-113-[0-9]{1,3}|127-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3})$`)
	ipv6AllowRe     = regexp.MustCompile(`^(?:2001:db8:[0-9a-f:]*|::1|00:00:5e:00:53:[0-9a-f]{2}|00:00:00:00:00:00)$`)
	reservedHostRe  = regexp.MustCompile(`^(?:(?:[a-z0-9-]+\.)*(?:example|test|invalid|localhost)|(?:[a-z0-9-]+\.)*example\.(?:com|net|org))$`)
	googleAPIsRe    = regexp.MustCompile(`^[a-z0-9-]+\.googleapis\.com$`)
	ecrHostAllowRe  = regexp.MustCompile(`^([0-9]{12})\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com$`)
	microsoftNSRe   = regexp.MustCompile(`^microsoft\.[a-z]+$`)
	corpusZoneRe    = regexp.MustCompile(`^(?:(?:[a-z0-9-]+\.)*supply-chain-demo\.internal|supply-chain-demo\.pagerduty\.internal|supply-chain-demo-project\.iam\.gserviceaccount\.com|supply-chain-demo-project\.uc\.r\.appspot\.com|supplychaindemoacr\.azurecr\.io|supply-chain-demo\.eastus\.azurecontainerapps\.io)$`)
	publicHostsList = map[string]struct{}{
		"github.com": {}, "gitlab.com": {}, "ghcr.io": {}, "registry.terraform.io": {}, "registry.npmjs.org": {},
		"proxy.golang.org": {}, "console.cloud.google.com": {}, "google.cloud": {}, "slsa.dev": {}, "in-toto.io": {},
		"kubernetes.io": {}, "app.kubernetes.io": {}, "argoproj.io": {}, "argocd.argoproj.io": {}, "us-docker.pkg.dev": {},
	}
)

// accountAllowed admits the AWS documentation account, the zero-prefixed
// forms, repdigits, and the reserved pseudonym form only when this run minted
// it.
func accountAllowed(account string, produced Set) bool {
	if docAccountRe.MatchString(account) || isRepdigit(account) {
		return true
	}
	return pseudoAccountRe.MatchString(account) && produced.Has(account)
}

func isRepdigit(s string) bool {
	if len(s) != 12 {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}

func ipv4Allowed(token string) bool   { return ipv4AllowRe.MatchString(token) }
func nodeIPAllowed(token string) bool { return nodeIPAllowRe.MatchString(token) }
func ipv6Allowed(token string) bool   { return ipv6AllowRe.MatchString(token) }

func hostnameAllowed(host string, produced Set) bool {
	if reservedHostRe.MatchString(host) || googleAPIsRe.MatchString(host) || microsoftNSRe.MatchString(host) || corpusZoneRe.MatchString(host) {
		return true
	}
	if _, ok := publicHostsList[host]; ok {
		return true
	}
	if match := ecrHostAllowRe.FindStringSubmatch(host); match != nil {
		return accountAllowed(match[1], produced)
	}
	return strings.HasSuffix(host, ".example")
}
