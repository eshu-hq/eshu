# Admin Provider Config

Administration for external identity providers: listing, detail, and
revision history (`ReadHandler`); create, update, revert, enable, disable,
and connection testing (`MutationHandler`).

Every route requires all-scope admin authentication. Secrets stay inside
the store boundary — responses and audit events carry presence flags, never
values. Providers managed by the environment are read-only. Shared audit
glue lives in the sibling `audit` package.
