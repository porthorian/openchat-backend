# Implementation Open Questions (Backend)

Last updated: 2026-03-03

## 1) SFU Engine Choice
- Should we implement SFU directly with Pion primitives or integrate Ion-SFU behind `rtc/sfu_adapter.go` first?
- What level of codec/simulcast control do we require for MVP vs post-MVP?

## 2) Authentication and Session Binding
- Current implementation accepts UID/device headers for join-ticket flow in non-production mode.
- What is the exact production session validation contract (token claims, issuer, expiry, revocation behavior)?
  JWT should be used

## 3) TURN/STUN Provisioning
- What TURN provider/deployment model should be the default for decentralized operators?
- Should TURN credentials always be ephemeral, and what TTL window should be enforced?

## 4) Room and Scaling Model
- At what participant threshold do we split signaling and SFU into separate worker processes?
- What room-affinity and worker-dispatch strategy should we standardize for horizontal scaling?

## 5) Moderation Enforcement Semantics
- For `timeout`, should backend enforce listen-only in RTC or fully deny room join based on policy?
- Should emergency `kick` require post-hoc moderation vote ratification in policy defaults?

## 6) Persistence and Audit Scope
- Which RTC events should be persisted by default (join/leave only vs richer signaling audit metadata)?
- Do we need retention/TTL policy for RTC audit records at MVP launch?

## 7) Signaling Protocol Versioning
- Should we support dual protocol versions during rollout (`protocol_version` N and N-1), or require strict cutover?
- What explicit deprecation policy should be documented for signaling message types?

## 8) Operational SLOs
- Confirm target SLOs for MVP:
  - join success rate
  - reconnect success rate
  - forced disconnect latency
  - negotiation error budget

## 9) Failure Recovery Behavior
- On backend restart, should we attempt session resume hints or always require full rejoin?
  Should attempt session resume hints
- Do we want bounded grace windows configurable per server profile for reconnect handling?

## 10) Postgres Persistence Rollout (New)
Reference docs:
- `postgres-persistence-rollout-plan.md`
- `postgres-feature-table-mapping.md`

Decided:
- Attachment/avatar backend supports object storage as an option; default storage is Postgres unless configured otherwise.
- Enforce strict channel-name uniqueness per group/category.
- `profiles:batch` should return only existing persisted profiles (no lazy create side effect).
- `auth_sessions` is source-of-truth in Postgres; Redis is a non-authoritative cache layer for hot-path session checks.
- Redis cache TTL should align with session expiry and use bounded jitter.
- Do not negative-cache invalid session tokens.

Remaining open decisions:
- Attachment/object-storage provider interface and cutover details.
- Exact case-folding/collation behavior for channel-name uniqueness constraints.

## 11) Seed Removal and Fixture Strategy (New)
- What is the final policy for development bootstrap fixtures (`OPENCHAT_ENABLE_SEED_BOOTSTRAP`) across CI/local/staging?
  Final policy is bootstrap should be done by creation of data instead of it being statically defined.
- Should we keep deterministic seed IDs for snapshot tests, or migrate all tests to generated fixtures and lookup by stable labels?
  Generated fixtures and lookup by stable labels
- When do we remove all in-memory `seed*` code paths versus keeping a temporary compatibility adapter?
  Immediately. This is because we are still working towards MVP and things can break at the moment.

## 12) Optional AT Protocol Authorization (New)
- Should DID be canonical user identity in storage, or remain provider subject mapped to local `user_uid`?
  Remain provider subject mapped to local user_uid
- Which AT Protocol claims/scopes are required for membership/channel/server mutation permissions?
- What revocation and refresh-token rotation SLA is required for production mode?
- Should `atproto_optional` fallback to dev headers in staging only, or also in production with explicit allowlist?

Auth/session decisions captured:
- Production session validation contract should use JWT.
