# Server actions and moderation threat model

Status: implementation design, 2026-09-18. Current channels carry plaintext. This contract permits future encrypted-channel reports without enabling encrypted messaging.

| Threat | Enforcement boundary | Required check |
| --- | --- | --- |
| UID spoofing | Server-issued sessions after Ed25519 challenge; legacy UIDs need separately verified binding | Reject header-only identity and replayed challenge |
| Stale membership or sanction | Every HTTP, realtime, and RTC join/publish authorization reads authoritative membership and active actions | Removed client cannot reconnect |
| Invite overuse | Redeem under a transaction and row lock; compare expiry, revocation, count, membership | Parallel redemption respects maximum |
| Private activity disclosure | Read-ack and privacy requests derive target UID from session | Other members cannot query cursors |
| Report evidence disclosure | Separate consent, authenticated encryption at rest, staff-scoped decrypt, 30-day post-closure erasure | Logs and audit contain no plaintext |
| Staff privilege abuse | Rank checks at action and enforcement; owner immune; policy owner-only | Equal/higher target denied |
| Vote races | Proposal row lock, one immutable vote per voter, action idempotency key and unique constraint | One sanction after concurrent votes |
| Old encrypted epoch after removal | Protected writes pause until membership epoch transition succeeds | Future encrypted-channel gate |

Audit metadata is append-only. A database transaction commits the sanction, session revocations, and outbox event together; delivery retries separately. A failed role, session, or membership lookup denies the action. Votes use policy fields snapshotted at proposal creation. Insufficient eligible voters blocks proposal creation, with no owner override. Sanctions remain unadvertised until durable state, identity recovery, and HTTP/realtime/RTC tests pass.
