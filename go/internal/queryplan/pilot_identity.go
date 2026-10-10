// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// PilotComparisonIdentity records the frozen Git roles for one methodology run.
// Target is the captured main or event base; Base is the measured comparison.
type PilotComparisonIdentity struct {
	Version   int    `json:"version"`
	Event     string `json:"event"`
	Mode      string `json:"mode"`
	Target    string `json:"target"`
	Base      string `json:"base"`
	Candidate string `json:"candidate"`
}

// LoadPilotComparisonIdentity reads the identity resolved before any producer.
func LoadPilotComparisonIdentity(path string) (PilotComparisonIdentity, error) {
	var identity PilotComparisonIdentity
	if path == "" {
		return identity, fmt.Errorf("missing ESHU_QUERY_METHODOLOGY_IDENTITY path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return identity, fmt.Errorf("read methodology identity: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return identity, fmt.Errorf("decode methodology identity: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return identity, fmt.Errorf("methodology identity has trailing JSON: %v", err)
	}
	if identity.Version != 1 || !pilotIdentityModeValid(identity.Event, identity.Mode) {
		return identity, fmt.Errorf("unsupported methodology identity version/event/mode")
	}
	for role, value := range map[string]string{"target": identity.Target, "base": identity.Base, "candidate": identity.Candidate} {
		if !pilotIdentitySHA(value) {
			return identity, fmt.Errorf("methodology identity has invalid %s SHA", role)
		}
	}
	if identity.Base == identity.Candidate {
		return identity, fmt.Errorf("methodology base equals candidate")
	}
	return identity, nil
}

func pilotIdentityModeValid(event, mode string) bool {
	switch event {
	case "local":
		return mode == "feature_merge_base"
	case "pull_request", "merge_group":
		return mode == "event_combined"
	case "push":
		return mode == "event_push_previous"
	case "schedule":
		return mode == "previous_main_snapshot"
	case "workflow_dispatch":
		return mode == "previous_main_snapshot" || mode == "feature_merge_base" || mode == "already_integrated_snapshot"
	default:
		return false
	}
}

func pilotIdentitySHA(value string) bool {
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// VerifyCandidateCheckout rejects a different checkout after identity freeze.
func (identity PilotComparisonIdentity) VerifyCandidateCheckout() error {
	output, err := exec.Command("git", "rev-parse", "--verify", "HEAD^{commit}").Output()
	if err != nil {
		return fmt.Errorf("resolve methodology candidate checkout: %w", err)
	}
	if actual := strings.TrimSpace(string(output)); actual != identity.Candidate {
		return fmt.Errorf("methodology candidate checkout changed: frozen %s actual %s", identity.Candidate, actual)
	}
	return nil
}
