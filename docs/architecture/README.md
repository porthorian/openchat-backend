# Backend Architecture Docs

## Status
- Maturity: `Pre-Alpha`
- Stability expectation: architecture plans and contracts may evolve before Beta.

## Documents
- `e2ee-epoch-model.md`: channel encryption and membership-epoch rules.
- `client-search-model.md`: search design with E2EE constraints.
- `multi-device-key-management.md`: key transfer and encrypted backup model.
- `backend-module-plan.md`: Go package/folder and service boundary plan.
- `mentions-read-ack-contract.md`: backend capability, API, and realtime contract for mentions + read acknowledgments.
- `moderation-and-governance.md`: moderation policy, voting, and enforcement design.
- `webrtc-sfu-backend-design.md`: signaling, SFU, ICE/TURN, and call lifecycle design.
- `webrtc-test-strategy.md`: contract, integration, load, and chaos testing plan for RTC.
- `helm-chart-release.md`: chart layout and `chart-vX.X.X` OCI publish flow.
- `postgres-persistence-rollout-plan.md`: Postgres rollout plan for currently implemented backend features, including seed removal and optional AT Protocol auth scaffolding.
- `postgres-feature-table-mapping.md`: feature-to-table persistence mapping matrix with confidence and TBD notes.
- `implementation-open-questions.md`: unresolved backend implementation decisions requiring maintainer input.
