# ansible-shared-roles

B-7 corpus fixture. A shared Ansible role repository (the `base` role) that other
playbook repositories depend on. It is the in-corpus target of the
`ansible-platform-playbooks` role reference, so the gate can resolve a
`DEPENDS_ON` edge backed by `ANSIBLE_ROLE_REFERENCE` evidence.

No proprietary data: all identifiers are synthetic.
