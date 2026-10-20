# CloudFront Service Package

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `types.go`
4. `scanner.go`
5. `relationships.go`
6. `awssdk/README.md`

Do not add object content reads, origin payload reads, policy document fetches,
certificate body reads, private key handling, or mutation APIs. Keep custom
origin header values out of facts; only header names are allowed.

The scanner boundary must remain `awscloud.ServiceCloudFront`. CloudFront is a
global service in the AWS control plane, so tests and runtime scopes should use
the configured global boundary instead of inferring application environment or
deployment truth.
