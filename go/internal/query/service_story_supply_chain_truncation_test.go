// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestServiceStoryAdmissibleImageIdentityFailsClosedWhenListTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]ContainerImageIdentityRow, 0, serviceStorySupplyChainReadLimit+1)
	for range serviceStorySupplyChainReadLimit + 1 {
		rows = append(rows, serviceStoryExactImageIdentity(serviceStoryTestDigest))
	}

	identity, reason := serviceStoryAdmissibleImageIdentity(rows)
	if got, want := reason, "container_image_identity_ambiguous"; got != want {
		t.Fatalf("reason = %q, want %q; identity=%#v", got, want, identity)
	}
}

func TestServiceStoryAdmissibleSBOMAttachmentsFailsClosedWhenListTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]SBOMAttestationAttachmentRow, 0, serviceStorySupplyChainReadLimit+1)
	for i := range serviceStorySupplyChainReadLimit + 1 {
		row := serviceStoryAttachedSBOM(serviceStoryTestDigest)
		row.AttachmentID = "sbom-attachment-truncated-" + string(rune('a'+i))
		rows = append(rows, row)
	}

	attachments, reason := serviceStoryAdmissibleSBOMAttachments(serviceStoryTestDigest, rows)
	if got, want := reason, "sbom_attachment_ambiguous"; got != want {
		t.Fatalf("reason = %q, want %q; attachments=%#v", got, want, attachments)
	}
}
