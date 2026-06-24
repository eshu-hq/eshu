// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/status"
)

var awsMaterializationDomains = map[string]struct{}{
	"aws_resource_materialization":                 {},
	"ec2_instance_node_materialization":            {},
	"aws_relationship_materialization":             {},
	"observability_coverage_materialization":       {},
	"security_group_cidr_materialization":          {},
	"security_group_rule_materialization":          {},
	"security_group_reachability_materialization":  {},
	"iam_can_assume_materialization":               {},
	"iam_escalation_materialization":               {},
	"iam_can_perform_materialization":              {},
	"s3_logs_to_materialization":                   {},
	"s3_external_principal_grant_materialization":  {},
	"rds_posture_materialization":                  {},
	"ec2_uses_profile_materialization":             {},
	"iam_instance_profile_role_materialization":    {},
	"ec2_internet_exposure_materialization":        {},
	"ec2_block_device_kms_posture_materialization": {},
	"s3_internet_exposure_materialization":         {},
}

func awsMaterializationStatusToMap(
	domains []status.DomainBacklog,
	blockages []status.QueueBlockage,
) map[string]any {
	blockedByDomain := queueBlockageCountsByDomain(blockages)
	domainRows := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	totals := domainWorkBuckets{}

	for _, backlog := range domains {
		if !isAWSMaterializationDomain(backlog.Domain) {
			continue
		}
		buckets := domainBacklogBuckets(backlog, blockedByDomain[backlog.Domain])
		totals.add(buckets)
		domainRows = append(domainRows, domainBacklogToMap(backlog, buckets))
		seen[backlog.Domain] = struct{}{}
	}
	for domain, blocked := range blockedByDomain {
		if !isAWSMaterializationDomain(domain) {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		buckets := domainWorkBuckets{Blocked: blocked}
		totals.add(buckets)
		domainRows = append(domainRows, map[string]any{
			"domain":      domain,
			"outstanding": 0,
			"pending":     0,
			"in_flight":   0,
			"blocked":     blocked,
			"retrying":    0,
			"dead_letter": 0,
			"failed":      0,
		})
	}

	sort.Slice(domainRows, func(i, j int) bool {
		return domainRows[i]["domain"].(string) < domainRows[j]["domain"].(string)
	})

	return map[string]any{
		"outstanding": totals.Outstanding,
		"pending":     totals.Pending,
		"in_flight":   totals.InFlight,
		"blocked":     totals.Blocked,
		"retrying":    totals.Retrying,
		"dead_letter": totals.DeadLetter,
		"failed":      totals.Failed,
		"domains":     domainRows,
	}
}

func isAWSMaterializationDomain(domain string) bool {
	_, ok := awsMaterializationDomains[domain]
	return ok
}

type domainWorkBuckets struct {
	Outstanding int
	Pending     int
	InFlight    int
	Blocked     int
	Retrying    int
	DeadLetter  int
	Failed      int
}

func (b *domainWorkBuckets) add(other domainWorkBuckets) {
	b.Outstanding += other.Outstanding
	b.Pending += other.Pending
	b.InFlight += other.InFlight
	b.Blocked += other.Blocked
	b.Retrying += other.Retrying
	b.DeadLetter += other.DeadLetter
	b.Failed += other.Failed
}

func domainBacklogBuckets(backlog status.DomainBacklog, blocked int) domainWorkBuckets {
	return domainWorkBuckets{
		Outstanding: backlog.Outstanding,
		Pending:     pendingDomainWork(backlog),
		InFlight:    backlog.InFlight,
		Blocked:     blocked,
		Retrying:    backlog.Retrying,
		DeadLetter:  backlog.DeadLetter,
		Failed:      backlog.Failed,
	}
}

func pendingDomainWork(backlog status.DomainBacklog) int {
	pending := backlog.Outstanding - backlog.InFlight - backlog.Retrying
	if pending < 0 {
		return 0
	}
	return pending
}

func queueBlockageCountsByDomain(blockages []status.QueueBlockage) map[string]int {
	counts := make(map[string]int)
	for _, blockage := range blockages {
		if blockage.Blocked > counts[blockage.Domain] {
			counts[blockage.Domain] = blockage.Blocked
		}
	}
	return counts
}

func domainBacklogToMap(backlog status.DomainBacklog, buckets domainWorkBuckets) map[string]any {
	return map[string]any{
		"domain":      backlog.Domain,
		"outstanding": buckets.Outstanding,
		"pending":     buckets.Pending,
		"in_flight":   buckets.InFlight,
		"blocked":     buckets.Blocked,
		"retrying":    buckets.Retrying,
		"dead_letter": buckets.DeadLetter,
		"failed":      buckets.Failed,
		"oldest_age":  backlog.OldestAge.Seconds(),
	}
}
