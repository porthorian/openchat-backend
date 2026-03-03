# Postgres Persistence Rollout Plan (v1)

Last updated: 2026-03-03
Status: Draft
Scope: Backend persistence for currently implemented features, with forward-compatible hooks for optional AT Protocol authorization and user-created servers/channels.

## 1) Objective
Move the backend from in-memory seeded state to durable Postgres persistence for all currently implemented product features, while preserving current API behavior during rollout.

This plan explicitly includes:
- Feature inventory grounded in current code.
- Data persistence boundaries and invariants.
- Seed data removal strategy.
- Transition to user-created servers/channels.
- Optional AT Protocol (`atproto`) authorization integration path.
- Implementation milestones/tickets.
- Explicit uncertainty tracking.

## 2) Current Implemented Backend Surface (Code-Backed)

### 2.1 Implemented endpoints
Current router (`internal/api/router.go`) includes:
- `GET /healthz`
- `GET /v1/client/capabilities`
- `GET /v1/rtc/signaling` (WS)
- `GET /v1/realtime` (WS)
- `GET /v1/servers`
- `GET /v1/servers/{serverID}/channels`
- `GET /v1/servers/{serverID}/members`
- `GET /v1/channels/{channelID}/messages`
- `POST /v1/channels/{channelID}/messages`
- `GET /v1/channels/{channelID}/attachments/{attachmentID}`
- `GET /v1/channels/{channelID}/mentions:resolve`
- `GET /v1/channels/{channelID}/read-ack`
- `PUT /v1/channels/{channelID}/read-ack`
- `DELETE /v1/servers/{serverID}/membership`
- `GET /v1/profile/me`
- `PUT /v1/profile/me`
- `POST /v1/profile/avatar`
- `GET /v1/profile/avatar/{assetID}`
- `GET /v1/profiles:batch`
- `POST /v1/rtc/channels/{channelID}/join-ticket`

### 2.2 Current storage behavior
Current persistence is in-memory only:
- Chat/domain state: `internal/chat/service.go`
- Mention/read-ack state: `internal/chat/mentions_readack.go`
- Profiles/avatars: `internal/profile/service.go`
- RTC ticket replay prevention: in-memory `usedJTIs` map in `internal/rtc/token_service.go`
- Realtime/RTC room membership and typing/presence: in-memory websocket hubs

Current seeded fixtures (in code):
- Seed servers (`srv_harbor`, `srv_testlab`)
- Seed channel groups/channels (`ch_general`, `vc_general`, etc.)
- Seed members
- Seed messages (`msg_seed_*`, `msg_tl_*`)

### 2.3 Immediate risks if not persisted
- Process restart loses all runtime writes.
- Multi-instance deployment cannot safely enforce RTC ticket replay protection.
- Read-ack monotonic correctness can diverge across replicas.
- Profile/version conflict checks are local-process only.

## 3) Persistence Principles and Invariants

### 3.1 Security and privacy invariants
- Preserve server-local isolation: no cross-server data leakage.
- Preserve UID-first contracts; do not require plaintext profile expansion for core messaging.
- Preserve E2EE direction: v1 schema allows plaintext current mode and E2EE envelope mode without forcing a future breaking migration.
- Store sensitive auth artifacts only as hashed/token-protected values.

### 3.2 Data correctness invariants
- Message history ordering must remain deterministic (`created_at`, `message_id` fallback).
- Read ack writes must remain monotonic per `(channel_id, user_uid)`.
- Message idempotency path must support safe retry semantics.
- Membership and channel/server relationships must be constrained with FKs.

### 3.3 Rollout invariants
- No hard cutover from seed/in-memory to DB on day one.
- Support phased migration gates with validation.
- Keep API payload compatibility wherever possible during migration.

## 4) v1 Persistence Scope

### 4.1 Persist in v1 (required)
- User identities (`users`)
- Servers and membership state (`servers`, `server_memberships`)
- Channel taxonomy (`channel_groups`, `channels`, `channel_memberships`)
- Messages and replies (`messages`)
- Mentions metadata (`message_mentions`)
- Message attachments metadata + bytes (`message_attachments`, `message_attachment_blobs`)
- Read-ack state (`channel_read_acks`)
- Profile canonical records + avatar assets (`profiles`, `profile_avatar_assets`, `profile_avatar_blobs`)
- Authorization identities/sessions scaffolding (`auth_identity_bindings`, `auth_sessions`)
- Optional AT Protocol auth flow state (`atproto_auth_requests`, `atproto_authorizations`)
- RTC join ticket replay protection (`rtc_join_ticket_replay`)

### 4.2 Persist later (explicitly deferred)
- Realtime typing state and live websocket presence snapshots.
- Long-term RTC room telemetry/audit events.
- Full moderation case/vote persistence (documented in architecture, not yet implemented in handlers).

### 4.3 Transitional compatibility
`messages` supports both:
- `encryption_mode='plaintext'` with `body_text`
- `encryption_mode='e2ee_envelope'` with `ciphertext` + `nonce` (+ optional `aad`, `epoch_id`)

This keeps current behavior while allowing a non-breaking path toward ciphertext-only channels.

## 5) Seed Data Removal and Replacement Plan

### 5.1 Why remove seeds
The current code path seeds static servers/channels/messages/members in process memory, which blocks:
- durable state,
- true multi-tenant/user-created topology,
- operator-controlled production initialization.

### 5.2 Planned removal phases
1. Introduce storage interfaces and Postgres repos behind existing services.
2. Remove in-memory constructor seeding immediately once DB-backed store wiring is introduced.
3. Move bootstrap data creation to explicit fixture/bootstrap workflows (idempotent DB writes), not constructor defaults.
4. Switch runtime read/write paths to Postgres.
5. Keep only fixture/bootstrap tooling for non-production setup; no implicit runtime seeding path.

### 5.3 Seed bootstrap policy (post-removal)
- Dev/local/CI/staging: bootstrap through explicit fixture creators and seed scripts, not static constructor seed data.
- Test: generated fixtures + stable labels for lookup; no hardcoded seed constants in app runtime.
- Production: no implicit seed data; initial admin/server creation is explicit.

### 5.4 Test impact from seed removal
Current tests directly reference seeded IDs (`msg_seed_01`, `srv_harbor`, `ch_general`).
Refactor plan:
- Replace hardcoded seed assumptions with test fixture setup helpers (DB-backed).
- Use generated fixtures with stable labels rather than static production-like IDs.
- Ensure tests run with an isolated DB schema per suite.

## 6) User-Created Servers and Channels Plan

### 6.1 Product direction alignment
Backend must support user-created servers/channels and no longer rely on static server/channel directory constants.

### 6.2 Backend API additions (planned)
- `POST /v1/servers` create server
- `PATCH /v1/servers/{serverID}` update server metadata
- `POST /v1/servers/{serverID}/channels` create channel
- `PATCH /v1/channels/{channelID}` update channel metadata
- `DELETE /v1/channels/{channelID}` soft-delete/archive channel

Note: these routes are not implemented yet; schema v1 includes required persistence structures.
Channel-name policy decision: enforce strict uniqueness per group/category (implemented via follow-up migration/index rules).

### 6.3 Creation defaults
When creating a server:
- Create creator user membership with owner/admin role.
- Create at least one default text channel (`general`) unless explicitly disabled by server creation payload.
- Record audit metadata (creator UID, timestamps).

### 6.4 Authorization hooks for creation
Server/channel creation permissions should resolve through auth context provider abstraction:
- `dev_header` mode (current compatibility)
- optional `atproto` mode

## 7) Optional AT Protocol Authorization Plan

### 7.1 Requirement
AT Protocol auth is optional and must coexist with existing non-production header-based identity mode.

### 7.2 Proposed auth modes
- `auth_mode=dev_header`: use current `X-OpenChat-User-UID` and `X-OpenChat-Device-ID` behavior.
- `auth_mode=atproto_optional`: accept either validated AT Protocol session or dev header fallback (environment-gated).
- `auth_mode=atproto_required` (future): disallow dev header in protected environments.

### 7.3 Persistence objects for atproto path
- `auth_identity_bindings`: maps local user UID to auth provider subject (for atproto: typically DID subject).
- `auth_sessions`: active/revoked session records with hashed token material.
- `atproto_auth_requests`: PKCE/state lifecycle records for login handshakes.
- `atproto_authorizations`: persisted authorization/token hash state per `(user_uid, did)`.

### 7.4 Request pipeline (target)
1. Resolve auth mode from config.
2. Attempt AT Protocol bearer validation when configured.
3. Map provider subject -> `auth_identity_bindings` -> local `user_uid`.
4. Create/update `auth_sessions` record.
5. Fall back to dev header mode only when policy allows.

### 7.5 Authorization decisions and remaining TBD
- Production session validation contract uses JWT tokens.
- AT Protocol identity remains provider subject mapped to local `user_uid` (not DID-as-canonical-user-id).
- TBD: exact AT Protocol claim set required for membership mutation routes.
- TBD: refresh token rotation and revocation SLA policy.

### 7.6 Session storage decision (adopted)
- `auth_sessions` in Postgres is the source of truth for session validity, expiry, and revocation.
- Redis is used as a read-through cache for hot-path session validation lookups.
- Cache data is non-authoritative: any cache error or cache miss falls back to Postgres.
- Revocation and refresh flow ordering is write Postgres first, then invalidate Redis keys.
- If Redis is unavailable, authentication continues in degraded mode using Postgres-only checks.

## 8) Implementation Sequence

### 8.1 Database and migration bootstrap
- Add migration runner invocation in startup path.
- Fail startup if required migrations cannot be applied in non-dev environments.
- Ensure migration lock strategy for concurrent deploys.

### 8.2 Storage abstraction rollout
Introduce interfaces per domain:
- `ServerStore`
- `ChannelStore`
- `MessageStore`
- `ReadAckStore`
- `ProfileStore`
- `AuthStore`
- `RTCTicketReplayStore`

Then implement Postgres-backed repositories and switch service wiring.

### 8.3 Phased cutover model
1. Write-through phase (optional): in-memory + DB writes with DB shadow reads for validation.
2. Read-primary DB phase: API reads from DB, fallback telemetry for missing data.
3. DB-only phase: remove in-memory mutating source of truth.

## 9) Operational and Performance Baseline

### 9.1 Required indexes (included in migration)
- Messages by channel/time: `(channel_id, created_at DESC, message_id DESC)`
- Read acks by user/channel
- Mentions by channel target
- Active sessions by user and expiry
- RTC replay table by expiry

### 9.2 Retention defaults (initial proposal)
- Messages and mentions: retain unless server policy says otherwise.
- Attachment blobs/avatar blobs: retain with soft-delete metadata; physical purge policy TBD.
- Session records: purge expired + revoked after configurable retention window.
- RTC replay rows: purge by expiry cron/maintenance task.

### 9.3 Redis caching policy for auth sessions
- Cache scope: `auth_sessions` lookups by token hash and session id.
- Cache shape: read-through on successful Postgres validation; no authoritative writes to Redis.
- Cache TTL policy: align cache TTL to remaining session expiry window with bounded jitter.
- Negative caching policy: do not negative-cache invalid tokens.
- Invalidation events: logout, revoke, refresh-token rotation, and explicit session expiry handling.
- Failure mode: treat Redis as optional infrastructure; never fail closed solely on cache unavailability.
- Observability requirement: track cache hit ratio, fallback-to-Postgres rate, and stale-cache deny events.

## 10) Detailed Milestone Ticket Plan

### M0: DB Foundation + Migrations
Deliverables:
- migration runner integration
- `migrations/000001_persistence_v1.{up,down}.sql`
- environment config for Postgres DSN and migration behavior
Acceptance criteria:
- backend starts with empty DB and applies v1 migration
- backend fails fast on migration mismatch in non-dev

### M1: Servers/Channels/Membership Persistence
Deliverables:
- Postgres-backed `ListServers`, `LeaveServerMembership`, `ListChannelGroups`, `ListMembers`
- remove in-memory seed constructors immediately
- seed/bootstrap moved to explicit fixture creation workflow
Acceptance criteria:
- existing endpoint response shapes preserved
- `DELETE /v1/servers/{serverID}/membership` persists across restart

### M2: Messages + Mentions + Attachments Persistence
Deliverables:
- persistent create/list message flows
- reply lookup by persisted message IDs
- attachment metadata + binary storage persisted (Postgres default, object storage backend optional via config)
- mention extraction persisted to `message_mentions`
Acceptance criteria:
- create message with attachment survives restart
- mention metadata query paths function on persisted data

### M3: Read Ack Persistence
Deliverables:
- monotonic read-ack writes persisted
- concurrency handling around stale updates
Acceptance criteria:
- stale cursor write is ignored after restart and across instances

### M4: Profile + Avatar Persistence
Deliverables:
- profile CRUD persistence with optimistic versioning
- avatar asset metadata + blob storage persistence (Postgres default, object storage backend optional via config)
Acceptance criteria:
- `If-Match` conflict behavior preserved
- uploaded avatar fetch still works after restart

### M5: Auth Persistence + Optional AT Protocol Integration Skeleton
Deliverables:
- persistence for `auth_identity_bindings`, `auth_sessions`
- Redis read-through cache for `auth_sessions` hot-path validation
- cache invalidation hooks for revoke/logout/refresh
- optional atproto request/session lifecycle persistence hooks
Acceptance criteria:
- auth session records created/revoked with expiry
- auth validation still succeeds with Redis unavailable (Postgres fallback)
- revoked sessions are denied with or without cache hit
- dev header compatibility maintained when configured

### M6: RTC Replay Durability
Deliverables:
- `TokenService.ParseAndConsume` replay tracking backed by DB
Acceptance criteria:
- replayed JTI is rejected across process restarts and replicas

### M7: Fixture and Legacy Seed Reference Cleanup
Deliverables:
- verify no in-memory seed constructor paths remain
- remove seed-dependent tests and replace with generated fixture builders + stable labels
Acceptance criteria:
- no production code path relies on hardcoded `seed*` functions
- test suite passes with DB fixtures only

### M8: User-Created Servers/Channels APIs
Deliverables:
- create/update/archive server/channel endpoints
- role checks on mutating routes
Acceptance criteria:
- new servers/channels are persisted and visible via list endpoints
- no static server list assumptions remain in code

## 11) Explicit Uncertainties and Open Questions

| ID | Topic | Current best default | Why uncertain | Decision needed by |
| --- | --- | --- | --- | --- |
| U-01 | Attachment/avatar storage backend rollout details | Postgres blob storage by default with optional object storage backend | Need concrete provider interface, migration/purge jobs, and rollout cutover strategy | Before high-volume rollout |
| U-02 | JWT claims and validation contract | JWT-based production sessions | Exact issuer/audience/clock-skew rules and key rotation strategy are still undefined | Before production auth hardening |
| U-03 | Session token lifecycle details | Hashed tokens in Postgres + Redis cache with TTL aligned to session expiry and no negative caching | Refresh rotation sequencing and revocation propagation SLO are undecided | Before production auth hardening |
| U-04 | Membership semantics for `listMembers` | Derive from server memberships + profile + ephemeral status | Current API member IDs/status are seed-shaped | Before removing legacy member fixtures |
| U-05 | Message body in plaintext mode | Keep plaintext column + E2EE columns | E2EE rollout timing not finalized | Before enabling protected channels by default |
| U-06 | Channel-name uniqueness implementation details | Enforce strict uniqueness per group/category | Need case-folding/collation normalization and migration constraints finalized | Before channel creation API GA |
| U-07 | RTC event/audit persistence | Defer | Scope can expand quickly; SLO/retention undecided | Before compliance requirements |
| U-08 | Bootstrap ownership model | First creator becomes owner role | Multi-admin bootstrap workflow not finalized | Before self-serve server creation launch |

## 12) Non-Goals for this rollout
- Implementing full moderation case persistence.
- Implementing voice/video media storage.
- Implementing full AT Protocol auth end-to-end in this single migration.
- Finalizing E2EE epoch tables and cryptographic key relay persistence in v1.
