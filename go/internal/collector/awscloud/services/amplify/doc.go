// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package amplify maps AWS Amplify app, branch, and custom-domain metadata into
// AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for Amplify apps and branches
// plus relationships for app-to-source-repository (an external git_repository
// join anchor), app-to-IAM-role (service role and SSR compute role),
// app-to-custom-domain (Route 53 hosted zone by normalized domain name and
// CloudFront distribution by *.cloudfront.net subdomain record), and
// branch-to-app evidence. Amplify environment variables, build-spec bodies,
// basic-auth credentials, repository access tokens, and every mutation,
// start-job, and start-deployment API stay outside this package contract.
// Repository URLs are reduced to host and path only so an embedded token never
// reaches a fact payload or a graph join key.
package amplify
