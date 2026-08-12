# The PARTIALLY comparable runtime-drift pair for the golden corpus (#5861).
#
# This lives in its own file, deliberately. Terraform reads every .tf in the
# directory, so config-owner resolution works identically either way -- but
# entity ids in the corpus are content-derived, and the B-12 snapshot pins
# `content-entity:e_1130fc33095d` for main.tf's aws_s3_bucket.data. Editing
# main.tf at all re-hashes that id and breaks the investigate_resource shape,
# which is a blast radius this fixture has no reason to incur.
#
# Why a Lambda and not another aws_instance: aws_instance is covered for exactly
# one comparable value (ami), so it can only ever produce a fully readable
# comparison or none at all. Lambda is covered for two -- image_uri and
# version -- which is the only shape where a pass can prove drift on one
# attribute while being unable to READ another. That is what #5861 changed, and
# without this pair the B-7 gate has nothing to assert it against: the behaviour
# would be exercised by unit tests only, and a regression in replay, payload
# writing, or HTTP/MCP readback of the coverage gap would ship green.
#
# The three sides are mismatched on purpose, each in a specific way:
#   - here (config): present, so cloudruntime.Classify has a resolvable owner
#     and reaches the value comparison at all.
#   - testdata/cassettes/terraformstate/supply-chain-demo.json: image_uri
#     present, version "9".
#   - testdata/cassettes/awscloud/supply-chain-demo.json: package_type "Image"
#     with NO image_uri (the unobservable side), version "7".
#
# That yields Degraded=[image_uri] and Drifted=[version], so the pair reports
# image_version_drift AND carries missing_evidence comparable_attribute:image_uri.
# Changing any one side without the others silently turns this into an ordinary
# drift or an inconclusive row and stops proving what it was added to prove.
resource "aws_lambda_function" "supply-chain-demo-partial" {
  function_name = "supply-chain-demo-partial"
  package_type  = "Image"
  image_uri     = "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:v9"
  role          = "arn:aws:iam::123456789012:role/supply-chain-demo-partial"
}
