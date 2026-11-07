# Agent instructions: querycontract/packet

Contract types and pure builders. No reader, no HTTP, no telemetry.

- This is a leaf the parent `query` package imports. Never import `query`
  from here; the parent's import of this leaf would become a cycle.
- The parent `querycontract` and the sibling `answer` and `evidence` leaves
  are the only eshu imports. Neither sibling imports this package back.
- HTTP responders (`packetBoundsFromRequest`, `writeInvestigationPacket`,
  `refusalPacketForAPI`, `NewSupplyChainImpactPacketResponder`,
  `NewDeployableUnitPacketResponder`, `getDriftPacket`) stay in the parent
  `query` package. Do not add `net/http` here.
- Packet bytes are a compatibility surface pinned by OpenAPI and CLI golden
  tests. Adding a field or a family requires its render, validate, and golden
  coverage in the same change.

Verify with `cd go && go vet ./internal/query/... && go test ./internal/query/querycontract/packet/ ./internal/query/ -count=1`.
