// Package ec2 emits AWS EC2 network topology fact evidence.
//
// Alongside the raw aws_resource and aws_relationship facts for VPCs, subnets,
// security groups, security-group rules, and network interfaces, the scanner
// emits one normalized aws_security_group_rule posture fact per rule. That fact
// carries the reachability tuple (group, direction, protocol, port range,
// normalized source) plus metadata-only derived booleans (is_internet for an
// exact open CIDR, is_all_protocols, is_all_ports). It is built from the same
// rule data already fetched for the raw facts, so it adds no AWS API calls, and
// it writes no graph edges: edge projection and internet-exposure analysis are
// later reducer and query slices.
package ec2
