// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"regexp"
	"sort"
	"strings"
)

var ecsECRHost = regexp.MustCompile(`^[0-9]{12}\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com$`)

// ECSContainerFact is one observed container in a running ECS task.
type ECSContainerFact struct {
	Name   string
	Image  string
	Digest string
}

// ECSTaskFact is one observed running ECS task, identified by its ARN.
type ECSTaskFact struct {
	ARN        string
	Containers []ECSContainerFact
}

// ECSTaskSummary reports a count only when task enumeration and image
// corroboration support the service's observed running count.
type ECSTaskSummary struct {
	Count             *int
	Reason            string
	MatchedContainer  string
	MatchedContainers []string
}

// SummarizeECSTasks checks an observed service running_count against distinct
// task ARNs and requires a matching ECR container digest before counting.
// Zero comes only from an observed service status with no running tasks.
func SummarizeECSTasks(runningCount int, tasks []ECSTaskFact, imageRefs []string) ECSTaskSummary {
	if runningCount < 0 {
		return ECSTaskSummary{Reason: "invalid_running_count"}
	}
	if runningCount == 0 {
		if len(tasks) != 0 {
			return ECSTaskSummary{Reason: "inconsistent_task_evidence"}
		}
		zero := 0
		return ECSTaskSummary{Count: &zero}
	}
	unique := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task.ARN == "" {
			return ECSTaskSummary{Reason: "task_arn_missing"}
		}
		unique[task.ARN] = struct{}{}
	}
	if len(unique) != runningCount {
		return ECSTaskSummary{Reason: "incomplete_task_evidence"}
	}
	wanted := make(map[string]struct{}, len(imageRefs))
	for _, ref := range imageRefs {
		ref = strings.TrimSpace(ref)
		if at := strings.LastIndex(ref, "@sha256:"); at >= 0 {
			ref = ref[at+1:]
		}
		if strings.HasPrefix(ref, "sha256:") {
			wanted[ref] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		hasECR := false
		hasContainer := false
		for _, task := range tasks {
			for _, container := range task.Containers {
				hasContainer = true
				host, _, hasPath := strings.Cut(container.Image, "/")
				hasECR = hasECR || (hasPath && ecsECRHost.MatchString(host))
			}
		}
		if !hasContainer {
			return ECSTaskSummary{Reason: "task_container_missing"}
		}
		if !hasECR {
			return ECSTaskSummary{Reason: "non_ecr_image_skipped"}
		}
		return ECSTaskSummary{Reason: "matching_digest_missing"}
	}
	seen := make(map[string]struct{}, len(tasks))
	matchedNames := make(map[string]struct{})
	for _, task := range tasks {
		if task.ARN == "" {
			return ECSTaskSummary{Reason: "task_arn_missing"}
		}
		if _, duplicate := seen[task.ARN]; duplicate {
			continue
		}
		seen[task.ARN] = struct{}{}
		match := false
		nonECR := false
		for _, container := range task.Containers {
			if _, ok := wanted[container.Digest]; !ok {
				continue
			}
			host, _, hasPath := strings.Cut(container.Image, "/")
			if !hasPath || !ecsECRHost.MatchString(host) {
				nonECR = true
				continue
			}
			match = true
			if strings.TrimSpace(container.Name) == "" {
				return ECSTaskSummary{Reason: "matching_container_name_missing"}
			}
			matchedNames[container.Name] = struct{}{}
		}
		if !match {
			if nonECR {
				return ECSTaskSummary{Reason: "non_ecr_image_skipped"}
			}
			return ECSTaskSummary{Reason: "matching_digest_missing"}
		}
	}
	if len(seen) != runningCount {
		return ECSTaskSummary{Reason: "incomplete_task_evidence"}
	}
	count := len(seen)
	names := make([]string, 0, len(matchedNames))
	for name := range matchedNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return ECSTaskSummary{Count: &count, MatchedContainer: names[0], MatchedContainers: names}
}
