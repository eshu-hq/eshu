// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import "strings"

// awsHostSuffixes are the AWS-owned DNS suffixes whose tail labels are kept
// (count = labels kept from the right, before the region search).
var awsHostSuffixes = []struct {
	suffix string
	tail   int
}{
	{suffix: ".amazonaws.com", tail: 3},
	{suffix: ".on.aws", tail: 2},
	{suffix: ".cloudfront.net", tail: 2},
	{suffix: ".awsapps.com", tail: 2},
}

// awsHostServiceLabels are labels left of the region in an AWS-owned host
// that name the service, not the customer, and are kept verbatim.
var awsHostServiceLabels = map[string]struct{}{
	"dkr": {}, "ecr": {}, "lambda-url": {}, "execute-api": {}, "s3": {}, "s3-website": {},
	"elb": {}, "rds": {}, "es": {}, "cache": {}, "sqs": {}, "sns": {}, "sts": {},
	"awsapprunner": {}, "appsync-api": {}, "elasticbeanstalk": {}, "cloudfront": {},
}

func lastLabelAlphabetic(host string) bool {
	host = strings.TrimSuffix(host, ".")
	label := host[strings.LastIndex(host, ".")+1:]
	if label == "" {
		return false
	}
	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// safeHost reports a hostname that already carries no organisation data:
// the reserved names and zones the private-data gate admits unconditionally.
func safeHost(lower string) bool {
	for _, suffix := range []string{".example", ".test", ".invalid", ".localhost", "example.com", "example.net", "example.org"} {
		if lower == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return lower == "localhost"
}

// learnHost pseudonymizes a DNS name label by label. Customer labels become
// h+10hex (class-free HMAC of the label), the public suffix collapses to
// "example", wildcard labels and a trailing dot are kept, and AWS-owned
// suffixes keep their structural tail.
func (d *dictionary) learnHost(raw string) {
	host := strings.TrimSuffix(raw, ".")
	trailingDot := strings.HasSuffix(raw, ".")
	lower := strings.ToLower(host)
	if safeHost(lower) {
		return
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		d.set(ClassIdent, raw, d.name(raw))
		return
	}
	var out []string
	if match := ecrHostRe.FindStringSubmatch(lower); match != nil {
		d.learnAccount(match[1])
		out = append([]string{d.account(match[1])}, labels[1:]...)
	} else if tail, aws := awsSuffixTail(lower); aws {
		out = d.awsHostLabels(labels, tail)
	} else {
		out = make([]string, 0, len(labels))
		for i, label := range labels {
			switch {
			case i == len(labels)-1:
				out = append(out, "example")
			case label == "*":
				out = append(out, label)
			default:
				out = append(out, "h"+d.key.hexOf(label, 10))
			}
		}
	}
	pseudonym := strings.Join(out, ".")
	if trailingDot {
		pseudonym += "."
	}
	d.set(ClassHost, raw, pseudonym)
}

func awsSuffixTail(lower string) (int, bool) {
	for _, candidate := range awsHostSuffixes {
		if strings.HasSuffix(lower, candidate.suffix) {
			return candidate.tail, true
		}
	}
	return 0, false
}

// awsHostLabels keeps the suffix tail, the region label and everything right
// of it, and the known service labels; account-shaped labels become account
// pseudonyms and every other customer label becomes h+10hex.
func (d *dictionary) awsHostLabels(labels []string, tail int) []string {
	keepFrom := len(labels) - tail
	for i := 0; i < keepFrom; i++ {
		if regionRe.MatchString(strings.ToLower(labels[i])) {
			keepFrom = i
			break
		}
	}
	out := make([]string, 0, len(labels))
	for i, label := range labels {
		lower := strings.ToLower(label)
		switch {
		case i >= keepFrom, label == "*":
			out = append(out, label)
		case account12Re.MatchString(label):
			d.learnAccount(label)
			out = append(out, d.account(label))
		default:
			if _, service := awsHostServiceLabels[lower]; service {
				out = append(out, label)
				continue
			}
			out = append(out, "h"+d.key.hexOf(label, 10))
		}
	}
	return out
}

// learnImageRef splits host, repository path, tag and digest. The host is a
// ClassHost value, the repository path is learned whole as an identifier,
// the tag and digest are structural and kept.
func (d *dictionary) learnImageRef(raw string) {
	ref, _, _ := strings.Cut(raw, "@")
	host, path, hasPath := strings.Cut(ref, "/")
	if !hasPath {
		d.learnIdent(ref)
		return
	}
	if strings.ContainsAny(host, ".:") || host == "localhost" {
		hostOnly, _, _ := strings.Cut(host, ":")
		d.learnHost(hostOnly)
	} else {
		d.learnIdent(host)
		path = host + "/" + path
	}
	if i := strings.LastIndex(path, ":"); i >= 0 && !strings.Contains(path[i:], "/") {
		path = path[:i]
	}
	d.learnIdent(path)
}
