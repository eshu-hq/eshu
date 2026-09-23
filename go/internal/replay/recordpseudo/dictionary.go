// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
)

var (
	awsIDRe     = regexp.MustCompile(`^(i|ami|sg|subnet|vpc|eni|vol|igw|rtb|acl|eipalloc|nat|vpce|snap|lt|asg|pl|tgw|vgw|cgw|vpn|fs|fsap|lb|tg)-[0-9a-f]{8,17}$`)
	hex32Re     = regexp.MustCompile(`^[0-9a-f]{32}$`)
	account12Re = regexp.MustCompile(`^[0-9]{12}$`)
	ipv4Re      = regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`)
	cidrRe      = regexp.MustCompile(`^[0-9A-Fa-f.:]+/[0-9]{1,3}$`)
	ecrHostRe   = regexp.MustCompile(`^([0-9]{12})\.dkr\.ecr\.([a-z0-9-]+)\.amazonaws\.com$`)
	numericRe   = regexp.MustCompile(`^[0-9]+$`)
	hostShapeRe = regexp.MustCompile(`^\*?[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+\.?$`)
	emailRe     = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// structuralWords are tokens that are never learned as identifiers: they are
// enum-like words that also appear in Keep fields (environment names, ELBv2
// load-balancer types, version qualifiers), so pseudonymizing one of them as
// a name would rewrite structural values elsewhere in the same recording.
var structuralWords = map[string]struct{}{
	"prod": {}, "production": {}, "stage": {}, "staging": {}, "dev": {}, "development": {},
	"test": {}, "qa": {}, "latest": {}, "default": {}, "main": {}, "master": {},
	"app": {}, "net": {}, "gwy": {}, "true": {}, "false": {}, "none": {}, "null": {},
	"$LATEST": {}, "aws": {}, "*": {}, "root": {},
}

// structural reports a token that is never learned: a structural word, an
// AWS service, resource-type or host-service word (awsVocabulary) or an
// AWS region / availability zone (awsRegionExactRe) -- the finite AWS
// grammar every scope id, ARN, stable key and ECR host is built from. A
// name or tag value equal to one carries no customer data and is kept.
func structural(raw string) bool {
	if _, ok := structuralWords[raw]; ok {
		return true
	}
	if _, ok := awsVocabulary[raw]; ok {
		return true
	}
	return awsRegionExactRe.MatchString(raw)
}

// entry is one learned token: its pseudonym and the class that shaped it.
type entry struct {
	pseudonym string
	class     Class
	// arnOnly marks a word learned only from a customer's spaced ARN name
	// (a CloudWatch alarm name). It is substituted everywhere except a Keep
	// value outside an ARN, so a common word there ("running") never
	// rewrites a Keep field such as state. A classified field that learns
	// the same word clears the mark.
	arnOnly bool
}

// classRank orders classes for precedence when one raw token is met under
// two classes: a structured shape (account, ARN, AWS id, name, host,
// address, email) always beats a tag value, so a resource whose Name tag
// equals its name is a name in every field and on every run. Keep, Opaque
// and Unknown never learn and rank lowest.
func classRank(class Class) int {
	switch class {
	case ClassKeep, ClassEnum, ClassOpaque, ClassUnknown:
		return 0
	case ClassTagValue:
		return 1
	default:
		return 2
	}
}

// dictionary learns raw tokens and their pseudonyms. It is single-goroutine:
// the source wrapper drives it, and it walks fields in sorted key order so
// the outcome never depends on map iteration.
type dictionary struct {
	key          Key
	entries      map[string]entry
	ipSlots      map[int]string
	ipCollisions int
	// accountSlots and accountCollisions are the account counterpart of
	// ipSlots/ipCollisions: the 10^8 pseudonym space is linear-probed so two
	// raw accounts never share a pseudonym within one recording.
	accountSlots map[uint64]string
	accountByRaw map[string]uint64
	// accountSpace is the account pseudonym space, accountSlotCount outside
	// tests; a test shrinks it to prove exhaustion fails closed.
	accountSpace      uint64
	accountCollisions int
	// failure is the first limit the recording exceeded (ErrIPv4Exhausted);
	// learn cannot return it, so the source reads it after the learning pass.
	failure error
	// unlistedARNTypes counts, per "service:token", the ARNs of a service
	// that has a type vocabulary but led with a token outside it: the token
	// was learned as a name, so the vocabulary miss is visible in the report.
	unlistedARNTypes map[string]int
	learned          map[Class]int
	sorted           []string
}

func newDictionary(key Key) *dictionary {
	return &dictionary{
		key:          key,
		entries:      map[string]entry{},
		ipSlots:      map[int]string{},
		accountSlots: map[uint64]string{},
		accountByRaw: map[string]uint64{},
		accountSpace: accountSlotCount,

		unlistedARNTypes: map[string]int{},
		learned:          map[Class]int{},
	}
}

// set records a pseudonym unless the token is structural (the one choke
// point every learner passes through) or already held by a class of equal
// or higher rank; a higher-ranked class replaces a lower one.
func (d *dictionary) set(class Class, raw, pseudonym string) {
	if raw == pseudonym || structural(raw) {
		return
	}
	if existing, ok := d.entries[raw]; ok {
		// A word first met only inside a spaced ARN name widens to every
		// field once a classified field learns it on its own.
		if classRank(existing.class) >= classRank(class) && !existing.arnOnly {
			return
		}
		d.learned[existing.class]--
	}
	d.entries[raw] = entry{pseudonym: pseudonym, class: class}
	d.learned[class]++
	d.sorted = nil
}

// pseudonym returns the learned pseudonym for raw, or raw itself.
func (d *dictionary) pseudonym(raw string) string {
	if existing, ok := d.entries[raw]; ok {
		return existing.pseudonym
	}
	return raw
}

// settled reports whether raw is already held by a class that outranks or
// equals the class asking to learn it.
func (d *dictionary) settled(class Class, raw string) bool {
	existing, ok := d.entries[raw]
	// An arnOnly entry is never settled, so a classified field can widen it.
	return ok && !existing.arnOnly && classRank(existing.class) >= classRank(class)
}

// learn classifies one raw value and records its pseudonym. Empty values,
// structural words and tokens already settled by an equal- or
// higher-ranked class are ignored.
func (d *dictionary) learn(class Class, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || d.settled(class, raw) || structural(raw) {
		return
	}
	switch class {
	case ClassAccount:
		d.learnAccount(raw)
	case ClassARN:
		d.learnARN(raw)
	case ClassIdent:
		d.learnIdent(raw)
	case ClassAWSID:
		d.learnAWSID(raw)
	case ClassHost:
		d.learnHost(raw)
	case ClassECRRef:
		d.learnImageRef(raw)
	case ClassIPv4:
		d.learnIPv4(raw)
	case ClassIPv6:
		d.learnIPv6(raw)
	case ClassCIDR:
		d.learnCIDR(raw)
	case ClassTagValue:
		d.learnTagValue(raw)
	case ClassEmail:
		d.learnEmail(raw)
	case ClassImageTag:
		d.learnImageTag(raw)
	case ClassEnum:
		if customerTypeName(raw) {
			// A customer-named type (Custom::<name>, <Org>::Svc::Res):
			// learn every component but the structural Custom and AWS.
			for _, component := range strings.Split(raw, "::") {
				if component != "" && component != "Custom" && component != "AWS" {
					d.learnIdent(component)
				}
			}
		}
	default:
		// Keep, Enum, Opaque and Unknown learn nothing: Keep and Enum values are structural,
		// the other two are replaced wholesale at rewrite time.
	}
}

// accountSlotCount is the size of the reserved 0000xxxxxxxx pseudonym space.
const accountSlotCount = 100000000

// account maps a raw account into the reserved 0000+8-digit form by HMAC,
// linear-probing past a slot already owned by a different raw account so
// two accounts never merge onto one pseudonym; every probe step is counted.
// The same raw account always returns the slot it owns without re-walking
// the probe, so a second mint (account field, then ECR host label) never
// counts the same collision twice.
func (d *dictionary) account(raw string) string {
	if idx, ok := d.accountByRaw[raw]; ok {
		return fmt.Sprintf("0000%08d", idx)
	}
	if uint64(len(d.accountSlots)) >= d.accountSpace {
		// Every slot is owned: fail the recording like IPv4 exhaustion
		// rather than overwrite a slot and merge two accounts' joins. The
		// returned value is never written, because drain stops on failure.
		if d.failure == nil {
			d.failure = fmt.Errorf("%w: recording exceeds %d distinct AWS accounts", ErrAccountsExhausted, d.accountSpace)
		}
		return "000000000000"
	}
	idx := binary.BigEndian.Uint64(d.key.mac(raw)) % d.accountSpace
	for {
		if _, used := d.accountSlots[idx]; !used {
			break
		}
		d.accountCollisions++
		idx = (idx + 1) % d.accountSpace
	}
	d.accountSlots[idx] = raw
	d.accountByRaw[raw] = idx
	return fmt.Sprintf("0000%08d", idx)
}

func (d *dictionary) name(raw string) string { return "n" + d.key.hexOf(raw, 11) }

func (d *dictionary) learnAccount(raw string) {
	if account12Re.MatchString(raw) {
		d.set(ClassAccount, raw, d.account(raw))
		return
	}
	d.learnIdent(raw)
}

// learnARN keeps partition, service, region and the leading resource-type
// token -- but only when the service's vocabulary (arnTypeTokens) names it:
// an SNS topic, an SQS queue or any unknown first component is a customer
// name and is learned. The ELBv2 and WAFv2 second-position type tokens are
// kept the same way (arnSecondTokens); every other component is learned.
// AWS-managed policies (account "aws") are public and stay whole; the
// account-root principal keeps "root" (a structural word).
func (d *dictionary) learnARN(raw string) {
	parts := strings.SplitN(raw, ":", 6)
	if len(parts) < 6 {
		d.learnIdent(raw)
		return
	}
	account := parts[4]
	if account == "aws" {
		return
	}
	if account != "" {
		d.learnAccount(account)
	}
	service, resource := parts[2], parts[5]
	if service == "s3" {
		// bucket/key/path/*: the bucket and every key-path component are
		// customer names; "*" and empty components are structural and
		// ignored by learnIdent, so the path shape survives.
		for _, component := range strings.Split(resource, "/") {
			d.learnARNComponent(component)
		}
		return
	}
	components := arnComponents(resource)
	skip := arnTypeSkip(service, components)
	if skip == 0 && len(components) >= 2 && len(arnTypeTokens[service]) > 0 {
		// A listed service whose ARN leads with a token outside its
		// vocabulary: the token is learned as a name below, and the miss
		// is reported (a service with no vocabulary leads with a customer
		// name, which is never reported).
		d.unlistedARNTypes[service+":"+components[0]]++
	}
	for i, component := range components[skip:] {
		if strings.Contains(component, " ") {
			d.learnSpacedComponent(component, service == "iam" && account == "cloudfront")
			continue
		}
		if i == 0 && numericRe.MatchString(component) {
			// The component at index skip is the resource name, not a
			// qualifier, so a numeric name seen only in an ARN is learned
			// here; learnIdent keeps the four-digit floor and the account
			// rule for a 12-digit name.
			d.learnIdent(component)
			continue
		}
		d.learnARNComponent(component)
	}
}

// arnComponents splits an ARN resource part on "/" and ":".
func arnComponents(resource string) []string {
	return strings.FieldsFunc(resource, func(r rune) bool { return r == '/' || r == ':' })
}

// arnTypeSkip is how many leading components of an ARN resource part are
// structural type tokens for service: 1 when the first is in the
// service's vocabulary, 2 when the second is a listed second-position
// token as well, else 0. The component at index skip is the resource
// name, whatever delimiter precedes it; only components after it can be
// qualifiers.
func arnTypeSkip(service string, components []string) int {
	if len(components) < 2 || !arnTypeTokens[service][components[0]] {
		return 0
	}
	if len(components) >= 3 && arnSecondTokens[service][components[1]] {
		return 2
	}
	return 1
}

// learnARNComponent learns one ARN resource component. A 12-digit
// component is an account (a foreign account in an S3 log key path, the
// member account of an organizations ARN) and is learned as one. Any other
// purely numeric component past the name is a qualifier (a task-definition
// revision, a function version, a date in an S3 key) and is never learned;
// learnARN learns a numeric name in the name position itself.
func (d *dictionary) learnARNComponent(component string) {
	if account12Re.MatchString(component) {
		d.learnAccount(component)
		return
	}
	if numericRe.MatchString(component) {
		return
	}
	d.learnIdent(component)
}

// learnIdent sniffs// learnTagValue: a tag value with a structured shape (an ARN in the
// cloudformation:stack-id tag, an account, an address, an email, a host) is
// learned by its structure so the same token in a structured field keeps
// its grammar; free-text values take the tag format.
func (d *dictionary) learnTagValue(raw string) {
	if structuredShape(raw) {
		d.learnIdent(raw)
		return
	}
	if numericRe.MatchString(raw) && len(raw) < minSubstituteLen {
		// The same rule as a numeric name: fewer than four digits carry no
		// customer data, and learning them as a tag would make a Name=317
		// tag rewrite the name field while the ARN keeps 317.
		return
	}
	d.set(ClassTagValue, raw, "t"+d.key.hexOf(raw, 11))
}

// structuredShape reports whether learnIdent would pick a structured
// pseudonym for raw rather than the plain name format.
func structuredShape(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "arn:"), strings.Contains(raw, ".dkr.ecr."), strings.Contains(raw, "@sha256:"):
		return true
	case cidrRe.MatchString(raw), awsIDRe.MatchString(raw), hex32Re.MatchString(raw), account12Re.MatchString(raw), ipv4Re.MatchString(raw), emailRe.MatchString(raw):
		return true
	case isIPv6(raw):
		return true
	case hostShapeRe.MatchString(raw) && lastLabelAlphabetic(raw):
		return true
	}
	return false
}

// learnIdent sniffs the shape of a name-like value and delegates.
func (d *dictionary) learnIdent(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || d.settled(ClassIdent, raw) || structural(raw) {
		return
	}
	switch {
	case strings.HasPrefix(raw, "arn:"):
		d.learnARN(raw)
	case strings.HasPrefix(raw, "workload:"):
		d.learnIdent(strings.TrimPrefix(raw, "workload:"))
	case strings.HasPrefix(raw, "service:") && !strings.Contains(raw, "/"):
		d.learnIdent(strings.TrimPrefix(raw, "service:"))
	case strings.Contains(raw, ".dkr.ecr."):
		d.learnImageRef(raw)
	case strings.HasPrefix(raw, "sha256:"):
		return
	case strings.Contains(raw, "@sha256:"):
		before, _, _ := strings.Cut(raw, "@")
		d.learnIdent(before)
	case cidrRe.MatchString(raw):
		d.learnCIDR(raw)
	case isIPv6(raw):
		d.learnIPv6(raw)
	case strings.Contains(raw, ":") && !strings.Contains(raw, "/"):
		// name:revision or repo:tag: the suffix is structural.
		before, _, _ := strings.Cut(raw, ":")
		d.learnIdent(before)
	case awsIDRe.MatchString(raw):
		d.set(ClassAWSID, raw, d.awsID(raw))
	case hex32Re.MatchString(raw):
		d.set(ClassAWSID, raw, d.key.hexOf(raw, 32))
	case account12Re.MatchString(raw):
		d.set(ClassAccount, raw, d.account(raw))
	case ipv4Re.MatchString(raw):
		d.learnIPv4(raw)
	case numericRe.MatchString(raw):
		// A purely numeric name of four or more digits is customer data
		// too. It takes the name form and is exact-only (substitute.go), so
		// it is rewritten as a whole value or a whole component, never
		// inside other digits and never in an ARN's ":"-qualifier position.
		// Fewer digits (a version, a revision, a count) carry no customer
		// data and would rewrite every equal qualifier: kept.
		if len(raw) >= minSubstituteLen {
			d.set(ClassIdent, raw, d.name(raw))
		}
	case emailRe.MatchString(raw):
		d.learnEmail(raw)
	case hostShapeRe.MatchString(raw) && lastLabelAlphabetic(raw):
		d.learnHost(raw)
	default:
		d.set(ClassIdent, raw, d.name(raw))
	}
}

func (d *dictionary) awsID(raw string) string {
	i := strings.Index(raw, "-")
	return raw[:i+1] + d.key.hexOf(raw, len(raw)-i-1)
}

func (d *dictionary) learnAWSID(raw string) {
	switch {
	case awsIDRe.MatchString(raw):
		d.set(ClassAWSID, raw, d.awsID(raw))
	case hex32Re.MatchString(raw):
		d.set(ClassAWSID, raw, d.key.hexOf(raw, 32))
	default:
		d.learnIdent(raw)
	}
}
