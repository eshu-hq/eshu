// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import "strings"

// locationTypeFromURI derives a coarse location type label from the DataSync
// location URI scheme so the scanner records the storage family without an
// extra describe read for unsupported flavors.
func locationTypeFromURI(uri string) string {
	trimmed := strings.TrimSpace(uri)
	scheme := trimmed
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		scheme = trimmed[:idx]
	}
	switch strings.ToLower(scheme) {
	case "s3":
		return "S3"
	case "efs":
		return "EFS"
	case "fsxl":
		return "FSX_LUSTRE"
	case "fsxn":
		return "FSX_ONTAP"
	case "fsxz":
		return "FSX_OPENZFS"
	case "fsxw":
		return "FSX_WINDOWS"
	case "fsxo":
		return "FSX_ONTAP"
	case "nfs":
		return "NFS"
	case "smb":
		return "SMB"
	case "hdfs":
		return "HDFS"
	case "azure-blob":
		return "AZURE_BLOB"
	case "":
		return ""
	default:
		return strings.ToUpper(scheme)
	}
}

// bucketFromS3URI extracts the bucket name from an `s3://bucket/prefix` URI.
func bucketFromS3URI(uri string) (string, bool) {
	return globalIDPrefix(uri, "s3://", "")
}

// efsFileSystemIDFromURI extracts the `fs-` file system id from an EFS location
// URI. DataSync encodes the EFS global id as `region.fs-id`, so the id is the
// trailing segment after the dot.
func efsFileSystemIDFromURI(uri string) (string, bool) {
	return globalIDPrefix(uri, "efs://", "fs-")
}

// fsxFileSystemIDFromURI extracts the `fs-` file system id from any FSx location
// URI (Lustre, ONTAP, OpenZFS, Windows). The id is the trailing `fs-` segment of
// the global id.
func fsxFileSystemIDFromURI(uri string) (string, bool) {
	trimmed := strings.TrimSpace(uri)
	idx := strings.Index(trimmed, "://")
	if idx < 0 || !strings.HasPrefix(strings.ToLower(trimmed[:idx]), "fsx") {
		return "", false
	}
	return globalIDPrefix(trimmed, trimmed[:idx]+"://", "fs-")
}

// globalIDPrefix returns the GLOBAL_ID segment of a `scheme://GLOBAL_ID/SUBDIR`
// DataSync location URI, taking the segment after the last dot when present, and
// requiring it to start with wantPrefix (empty wantPrefix accepts any segment).
func globalIDPrefix(uri, scheme, wantPrefix string) (string, bool) {
	trimmed := strings.TrimSpace(uri)
	if !strings.HasPrefix(trimmed, scheme) {
		return "", false
	}
	remainder := strings.TrimPrefix(trimmed, scheme)
	if slash := strings.IndexByte(remainder, '/'); slash >= 0 {
		remainder = remainder[:slash]
	}
	if wantPrefix != "" {
		if dot := strings.LastIndexByte(remainder, '.'); dot >= 0 {
			remainder = remainder[dot+1:]
		}
	}
	remainder = strings.TrimSpace(remainder)
	if remainder == "" {
		return "", false
	}
	if wantPrefix != "" && !strings.HasPrefix(remainder, wantPrefix) {
		return "", false
	}
	return remainder, true
}

// firstNonEmpty returns the first trimmed non-empty string in values, or "".
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
