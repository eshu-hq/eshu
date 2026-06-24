// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultapi

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
)

const (
	kvV2Type        = "kv"
	kvMaxRecursion  = 32
	kvMaxPathsScan  = 50000
	customMetaLimit = 256
)

// ListKVMetadata returns KV v2 metadata-path descriptions for every kv-v2
// secret engine, sourced from the metadata endpoint only. It never reads a KV
// data value (doRequest also rejects any /data/ path defensively).
func (a *Adapter) ListKVMetadata(ctx context.Context) (_ []vaultlive.KVMetadata, err error) {
	defer func() { a.recordAPICall("list_kv_metadata", err) }()
	mounts, err := a.ListSecretEngineMounts(ctx)
	if err != nil {
		return nil, err
	}
	var out []vaultlive.KVMetadata
	scanned := 0
	for _, mount := range mounts {
		if !isKVV2(mount) {
			continue
		}
		mountPath := mount.MountPath
		metaRoot := strings.TrimRight(mountPath, "/") + "/metadata"
		if err := a.walkKVMetadata(ctx, mountPath, metaRoot, "", 0, &scanned, &out); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MountPath != out[j].MountPath {
			return out[i].MountPath < out[j].MountPath
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func isKVV2(mount vaultlive.SecretEngineMount) bool {
	return strings.EqualFold(mount.MountType, kvV2Type) && mount.KVVersion == "2"
}

// walkKVMetadata recursively lists KV v2 metadata under prefix. Directory keys
// end with "/"; leaf keys are read for their metadata. Recursion and total
// scanned paths are bounded so a deep or adversarial tree cannot run away.
func (a *Adapter) walkKVMetadata(
	ctx context.Context,
	mountPath, metaRoot, prefix string,
	depth int,
	scanned *int,
	out *[]vaultlive.KVMetadata,
) error {
	if depth >= kvMaxRecursion || *scanned >= kvMaxPathsScan {
		return nil
	}
	listPath := metaRoot
	if prefix != "" {
		// Escape the accumulated prefix the same way the read path is escaped,
		// so a Vault-returned key name cannot inject query params or special
		// characters into the LIST request. Traversal segments are rejected by
		// doRequest's hasTraversalSegment guard.
		listPath = metaRoot + "/" + pathEscapePath(strings.TrimRight(prefix, "/"))
	}
	keys, err := a.listKeys(ctx, listPath)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if *scanned >= kvMaxPathsScan {
			return nil
		}
		*scanned++
		child := prefix + key
		if strings.HasSuffix(key, "/") {
			if err := a.walkKVMetadata(ctx, mountPath, metaRoot, child, depth+1, scanned, out); err != nil {
				return err
			}
			continue
		}
		meta, ok, err := a.readKVMetadata(ctx, mountPath, metaRoot, child)
		if err != nil {
			return err
		}
		if ok {
			*out = append(*out, meta)
		}
	}
	return nil
}

func (a *Adapter) readKVMetadata(
	ctx context.Context,
	mountPath, metaRoot, path string,
) (vaultlive.KVMetadata, bool, error) {
	var payload struct {
		Data struct {
			CurrentVersion     int             `json:"current_version"`
			OldestVersion      int             `json:"oldest_version"`
			MaxVersions        int             `json:"max_versions"`
			CASRequired        bool            `json:"cas_required"`
			DeleteVersionAfter json.RawMessage `json:"delete_version_after"`
			CustomMetadata     map[string]any  `json:"custom_metadata"`
		} `json:"data"`
	}
	ok, err := a.doRequest(ctx, metaRoot+"/"+pathEscapePath(path), false, &payload)
	if err != nil || !ok {
		return vaultlive.KVMetadata{}, false, err
	}
	return vaultlive.KVMetadata{
		MountPath:              mountPath,
		Path:                   path,
		CurrentVersion:         payload.Data.CurrentVersion,
		OldestVersion:          payload.Data.OldestVersion,
		MaxVersions:            payload.Data.MaxVersions,
		CASRequired:            payload.Data.CASRequired,
		DeleteVersionAfterSecs: durationSeconds(payload.Data.DeleteVersionAfter),
		CustomMetadataKeys:     boundedMapKeys(payload.Data.CustomMetadata),
	}, true, nil
}

// pathEscapePath escapes each segment of a multi-segment KV path while keeping
// the separators.
func pathEscapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = pathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// boundedMapKeys returns the sorted keys of m, capped so a pathological
// custom-metadata map cannot blow up the fact.
func boundedMapKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > customMetaLimit {
		keys = keys[:customMetaLimit]
	}
	return keys
}
