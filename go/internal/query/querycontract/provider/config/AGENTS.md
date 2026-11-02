# querycontract/provider/config

Read `go/internal/query/querycontract/AGENTS.md` first. The provider-config
contract rules here add to it; on a conflict the parent wins.

- This leaf owns only the seven provider-config DTO structs. It owns no
  port interface, handler, route, store implementation, or SQL.
- Keep the package stdlib-only. The ports and their sentinel errors live
  in `internal/query/admin/provider/config`; do not import that package
  from here — the family's in-package tests import `testutil`, which
  imports this leaf, and a back-edge is an import cycle in the test
  binary (#7715).
- Add a DTO field here in the same change that teaches its producer and
  consumer; the family's alias list is the compatibility surface.
