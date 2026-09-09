# Admin Tenant Identity

Tenant-scoped identity administration: invitations, membership roles and
their grants, external identity providers and their group-to-role mappings,
generated API tokens, and tenant audit reads (`ReadHandler`), plus the
corresponding writes (`MutationHandler`).

Every route requires all-scope admin authentication and stays within the
caller's own tenant; allowed and denied mutations both emit governance
audit events. Shared audit glue lives in the sibling `audit` package.
