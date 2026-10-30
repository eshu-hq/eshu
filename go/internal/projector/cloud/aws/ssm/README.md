# SSM host inventory projector intents

Builds the `ssm_host_inventory_materialization` reducer intent from one
immutable scope generation (issue #9558).

One scope-keyed intent fires when any admitted `aws_ssm_host_application` or
`aws_ssm_host_platform` fact is present, anchored to the earliest application
fact (platform fallback). Either kind triggers alone. The entity key is
domain-distinct and shared with no other domain.
