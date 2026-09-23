// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/replay/recorder"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// Review finding F6 on 40a4ac36e (verdict-p3.md).

// TestKeyNeverFormatsItsMaterial (F6): %v, %+v, %#v and slog of Key, Config
// and recorder.Options show the fingerprint only.
func TestKeyNeverFormatsItsMaterial(t *testing.T) {
	key := mustKey(t, keyA)
	cfg := recordpseudo.Config{Key: key, Policy: recordpolicy.Policy()}
	opts := recorder.Options{Path: "x", Pseudonymize: &cfg}
	renderings := []string{
		fmt.Sprintf("%v %+v %#v %s", key, key, key, key),
		fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg),
		fmt.Sprintf("%v %+v %#v", opts, opts, opts),
		fmt.Sprintf("%v %+v %#v", *opts.Pseudonymize, *opts.Pseudonymize, *opts.Pseudonymize),
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("key", "key", key, "cfg", cfg)
	renderings = append(renderings, logs.String())
	for i, rendered := range renderings {
		if strings.Contains(rendered, keyA) || strings.Contains(rendered, "6965-p3-key") {
			t.Errorf("rendering %d exposes key material", i)
		}
		// Rendering 2 formats recorder.Options, whose Pseudonymize is a
		// pointer and prints as an address; every other rendering shows the
		// Key and must show its fingerprint.
		if i != 2 && !strings.Contains(rendered, key.Fingerprint()) {
			t.Errorf("rendering %d does not carry the fingerprint", i)
		}
	}
}
