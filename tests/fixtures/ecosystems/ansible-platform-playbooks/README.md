# ansible-platform-playbooks

B-7 corpus fixture. An Ansible playbook repository whose `playbooks/site.yml`
applies the shared `ansible-shared-roles` role. The role reference resolves to
the in-corpus `ansible-shared-roles` repository, producing an
`ANSIBLE_ROLE_REFERENCE` evidence fact and a
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge.

The golden-corpus gate asserts this edge filtered on
`evidence_kinds=[ANSIBLE_ROLE_REFERENCE]`, isolating the Ansible role-dependency
verb from the package-consumption `DEPENDS_ON` edges (rc-3).

No proprietary data: all identifiers are synthetic.
