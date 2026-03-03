# Multi-Instance Scaling and Sharding Plan

Last updated: 2026-03-03
Status: Draft
Scope: Scale backend operation beyond a single instance while preserving server isolation and current API semantics.

## 1) Objective
Define a pragmatic path from single-instance operation to multi-instance and then sharded operation, with `server_id` as the primary distribution key.

This plan focuses on:
- horizontal API scaling,
- shared realtime/RTC coordination across instances,
- data sharding by `server_id`,
- operational migration/rebalance mechanics.

## 2) Current Constraints (Code-Backed)
Current implementation is not multi-instance safe in several areas:
- realtime room presence/typing fanout is process-local (`internal/realtime/chat_hub.go`),
- RTC signaling room membership is process-local (`internal/rtc/signaling_service.go`),
- some runtime assumptions still originate from single-process seeded behavior.

Consequence:
- running multiple API/WS nodes behind a load balancer can cause inconsistent fanout/session behavior unless coordination is externalized.

## 3) Scaling Principles
- Preserve server isolation: `server_id` is the tenancy boundary.
- Prefer stateless API instances.
- Keep Postgres authoritative for durable state.
- Use Redis as cache/bus where low-latency coordination is needed, but keep cache non-authoritative.
- Avoid cross-shard distributed transactions.
- Keep shard routing deterministic and observable.

## 4) Target Topology (High-Level)
1. Edge/load balancer layer:
- L7 routing to API/WS nodes.
- Sticky sessions only where required (WS/RTC optimization), not as correctness crutch.

2. API/WS nodes (many):
- Stateless request handlers.
- Shared auth/session validation rules.
- Shared shard-routing resolver.

3. Shared infrastructure:
- Postgres (initial single cluster, later multiple shard clusters).
- Redis (session cache + realtime pub/sub and coordination).

4. Optional control-plane service (later phase):
- shard map authority,
- rebalance orchestration,
- placement and routing validation.

## 5) Phase Plan

### Phase A: Multi-Instance Readiness (No DB Sharding Yet)
Goal:
- run many API/WS instances safely against one Postgres deployment.

Required changes:
- remove remaining in-memory authoritative state paths,
- externalize realtime fanout (publish event once, consume by all interested nodes),
- externalize RTC room ownership/coordination metadata,
- keep join-ticket replay checks globally consistent.

Outcomes:
- N API instances can serve the same server safely.
- instance restart does not lose durable correctness.

### Phase B: Logical Sharding by `server_id`
Goal:
- introduce shard mapping and route each server to a target data plane.

Required changes:
- shard map model: `server_id -> shard_id -> connection profile`,
- request path resolution to shard before DB operations,
- strict rule that server-scoped mutations stay within one shard.

Outcomes:
- shard placement becomes explicit and operable.
- new servers can be placed on target shards by policy.

### Phase C: Physical Sharding + Rebalancing
Goal:
- distribute data and load across multiple Postgres clusters.

Required changes:
- shard-aware connection management and health checks,
- rebalancing tooling for server moves,
- operational playbooks (drain, copy, verify, cutover, rollback).

Outcomes:
- predictable horizontal growth without single-cluster bottleneck.

## 6) Shard Key and Data Locality
Primary shard key:
- `server_id`.

Reasoning:
- current APIs are largely server-scoped or channel-scoped and channels belong to servers,
- keeps high-volume entities (`messages`, `mentions`, `read_acks`) local to one shard.

Data locality rules:
- `servers`, `server_memberships`, `channels`, `messages`, `message_mentions`, `read_acks` colocated by `server_id`,
- cross-server data (if any in future) should be control-plane or asynchronously aggregated.

## 7) Routing and Resolution Model

### 7.1 Request routing
- For routes with `serverID` path params, resolve shard directly from `server_id`.
- For routes with `channelID` only, resolve `channel_id -> server_id` first, then resolve shard.

### 7.2 Suggested resolver cache
- in-process short TTL cache for `server_id -> shard` and `channel_id -> server_id`,
- invalidation on control-plane updates,
- cache misses always fallback to authoritative store.

### 7.3 Failure behavior
- If shard resolution fails, return explicit retryable errors for transient cases.
- If shard is marked draining/moving, enforce controlled write policy (see rebalance section).

## 8) Realtime and RTC Multi-Instance Strategy

### 8.1 Realtime event fanout
- keep canonical events persisted first (where applicable),
- publish fanout envelopes onto shared bus (Redis pub/sub or streams),
- each WS node fans out only to locally connected subscribers.

### 8.2 RTC room ownership
- assign each active room a logical owner node (lease-based) for authoritative signaling coordination,
- non-owner nodes forward signaling/control messages to owner path.

### 8.3 Restart and resume behavior
- after node or backend restart, attempt session resume hints first,
- if resume cannot be satisfied, enforce full rejoin fallback.

## 9) Shard Metadata and Control Plane
Minimum shard metadata (initial):
- `shard_id`
- `db_dsn`/connection profile reference
- status (`active`, `draining`, `read_only`, `offline`)
- capacity hints (soft limits)

Server assignment metadata:
- `server_id`
- `shard_id`
- `assigned_at`
- optional `move_state` for migration.

Note:
- control metadata can begin in a global Postgres catalog and later move to dedicated control-plane service if needed.

## 10) Rebalancing Plan (Server Move Between Shards)
Recommended sequence:
1. Mark server assignment as `move_pending`.
2. Enable write controls for affected server (short maintenance window or queued writes strategy).
3. Snapshot/copy server-scoped data to target shard.
4. Verify row counts + integrity checks.
5. Switch shard assignment atomically.
6. Re-enable writes and monitor error budget.
7. Keep rollback window with source snapshot.

Tradeoff:
- simpler maintenance-window cutover is preferred first; live dual-write migration can be added later if required.

## 11) Data and Storage Scale Considerations
- Keep Postgres default blob storage support for simple deployments.
- Offer optional object-storage backend for attachments/avatars to reduce DB storage pressure.
- For very high message volume, combine shard-by-`server_id` with table partitioning by time within each shard.

## 12) Security and Auth Implications
- JWT validation must be stateless at edge/API nodes.
- `auth_sessions` stays authoritative in Postgres.
- Redis cache remains non-authoritative with invalidate-on-revoke behavior.
- AT Protocol identity mapping remains provider subject -> local `user_uid`.

## 13) Observability and SLO Additions
Add metrics:
- shard resolution latency/error rate,
- per-shard query latency and saturation,
- rebalance job duration/success/failure,
- realtime fanout lag,
- RTC room-owner handoff rate/failures.

Minimum dashboards:
- per-shard health,
- hotspot servers/channels,
- cache hit/miss and DB fallback for auth/session checks.

## 14) Milestone Ticket Plan

### S0: Multi-Instance Readiness Baseline
Deliverables:
- define instance-safe invariants and failure semantics
- identify/remove remaining process-local authoritative state
Acceptance criteria:
- two or more API instances can run without correctness regressions in core messaging/read-ack/profile paths

### S1: Shared Realtime Bus
Deliverables:
- shared pub/sub fanout for chat events
- node-local subscriber fanout adapters
Acceptance criteria:
- subscribers connected to different instances receive consistent events

### S2: RTC Room Ownership Coordination
Deliverables:
- lease-based room ownership metadata
- forward-to-owner path for signaling operations
Acceptance criteria:
- multi-instance RTC signaling remains consistent under node churn

### S3: Shard Map and Resolver
Deliverables:
- shard assignment model
- runtime shard resolver with local cache + invalidation hooks
Acceptance criteria:
- server-scoped requests route deterministically to assigned shard

### S4: Multi-Shard Data Plane
Deliverables:
- support multiple Postgres shard connection pools
- shard-aware repository wiring
Acceptance criteria:
- create/list/write flows function on more than one active shard

### S5: Server Rebalance Tooling
Deliverables:
- controlled server move workflow
- verification and rollback scripts/runbook
Acceptance criteria:
- one test server can be moved between shards with no data loss

### S6: Scale Hardening
Deliverables:
- per-shard autoscaling and alerting policies
- hotspot detection + placement strategy
Acceptance criteria:
- sustained load tests meet agreed SLOs across multiple shards

## 15) Open Questions
- Should realtime bus be Redis pub/sub first or Redis streams first?
- Do we require strict sticky sessions for WS/RTC, or only best-effort affinity?
- What is the initial threshold for promoting from Phase A to Phase B?
- Should shard assignment be operator-managed first or policy-driven auto-placement?
- For rebalance, do we allow write queueing, short write freeze, or dual-write from day one?

