// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ses maps Amazon Simple Email Service (SES v2) email-identity,
// configuration-set, configuration-set event-destination, and dedicated-IP-pool
// metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for SES email identities,
// configuration sets, configuration-set event destinations, and dedicated IP
// pools, plus relationships for the identity-to-default-configuration-set,
// configuration-set-to-dedicated-IP-pool, event-destination-to-SNS-topic, and
// event-destination-to-Firehose-delivery-stream edges (and, defensively, an
// identity-DKIM-to-KMS-key edge if AWS ever reports a customer key). Sending
// email, message and template bodies, DKIM private keys, DKIM signing tokens,
// identity policy documents, and SMTP credentials stay outside this package
// contract: the scanner is metadata-only.
package ses
