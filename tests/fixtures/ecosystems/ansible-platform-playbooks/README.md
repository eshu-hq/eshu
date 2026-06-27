# ansible-platform-playbooks

B-7 corpus fixture. An Ansible playbook repository whose `playbooks/site.yml`
applies the `base` role with an explicit git `src:` pointing at the in-corpus
`ansible-shared-roles` repository. Resolution runs through the real repo-source
path (the `src:` URL tokens match the `ansible-shared-roles` catalog entry, not a
contrived role-name match), producing an `ANSIBLE_ROLE_REFERENCE` evidence fact
and a
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge.

The golden-corpus gate asserts this edge filtered on
`evidence_kinds=[ANSIBLE_ROLE_REFERENCE]`, isolating the Ansible role-dependency
verb from the package-consumption `DEPENDS_ON` edges (rc-3).

No proprietary data: all identifiers are synthetic.
