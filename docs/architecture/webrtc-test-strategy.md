# WebRTC Test Strategy (Backend)

## 1) Purpose
Define backend test coverage for SFU-first WebRTC implementation in `openchat-backend`, including signaling correctness, permission enforcement, moderation integration, and operational reliability.

## 2) Test Objectives
- Verify secure and deterministic join-ticket issuance/validation.
- Verify signaling protocol correctness and idempotency.
- Verify room and track lifecycle behavior under normal and failure conditions.
- Verify moderation actions (`kick`/`ban`/`timeout`) are enforced immediately in RTC flows.
- Verify reliability and performance thresholds before production rollout.

## 3) Scope
### In Scope
- Unit tests for `rtc/*` services.
- Integration tests for API + signaling + SFU adapter behavior.
- Contract tests for capability payload and signaling schema.
- Load, soak, and chaos tests for operational hardening.

### Out of Scope
- Codec-level subjective media quality evaluation.
- TURN infrastructure vendor testing beyond interface assumptions.
- Frontend UI behavior validation (client repo responsibility).

## 4) Test Layers
1. Contract tests
2. Unit tests
3. Integration tests
4. End-to-end multi-client smoke tests
5. Load/soak tests
6. Chaos/resilience tests

## 5) Contract Test Matrix
Validate schema for:
- `GET /client/capabilities` with `rtc` object
- `POST /v1/rtc/channels/:channel_id/join-ticket` request/response
- signaling message envelope fields (`type`, `request_id`, `channel_id`, `payload`)
- canonical error codes:
  - `rtc_join_denied`
  - `rtc_ticket_expired`
  - `rtc_ticket_replayed`
  - `rtc_permission_denied`
  - `rtc_negotiation_failed`

Fixture pack (suggested):
- `tests/contracts/rtc/capabilities/*.json`
- `tests/contracts/rtc/signaling/*.json`
- `tests/contracts/rtc/errors/*.json`

## 6) Unit Test Matrix
### `rtc/token_service.go`
- issues ticket with expected claims
- enforces TTL expiry
- enforces one-time `jti` replay protection
- rejects mismatched `server_id`/`channel_id`

### `rtc/media_policy.go`
- grants/denies publish rights by role and moderation state
- timeout maps to listen-only policy when configured
- channel lock blocks new join tickets

### `rtc/room_service.go`
- join/leave transitions
- track publish/unpublish state updates
- stale participant cleanup after grace timeout

### `rtc/signaling_service.go`
- request/response correlation by `request_id`
- duplicate client message idempotency
- deterministic error mapping

## 7) Integration Test Matrix
Run in-process API + signaling + SFU adapter test double:
- valid join path with voice publish/subscribe
- invalid join ticket (expired/replayed) rejected
- concurrent joins in same room maintain consistent participant count
- ICE candidate forwarding correctness
- reconnect within grace window resumes session state
- reconnect beyond grace window creates fresh join sequence
- moderator kick forces disconnect and room eviction
- banned user cannot mint new join ticket
- timeout user cannot publish but can remain subscribed (policy-dependent)

## 8) End-to-End Smoke Matrix
With Dockerized Postgres + TURN + backend:
1. 3 participants join channel and establish audio.
2. one participant enables video; others receive track event.
3. one client network drop + reconnect recovery.
4. moderator kick event removes target within SLO.
5. post-ban rejoin attempt denied.

## 9) Load and Soak Tests
Scenarios:
- 50, 100, 200 participant synthetic rooms (configurable by environment size).
- mixed voice/video profile (for example 80% voice-only, 20% video).
- 60-minute soak runs to catch leaks and goroutine buildup.

Measure:
- join success rate
- signaling RTT p50/p95
- publish negotiation failure rate
- SFU worker CPU/memory
- forced disconnect latency

## 10) Chaos and Fault Injection
- random signaling socket disconnects
- packet loss and latency injection in test network
- TURN credential expiration mid-session
- backend process restart during active rooms
- Postgres transient failures on audit writes

Expected behavior:
- controlled degradation, no panic crashes
- predictable error responses
- eventual room cleanup without orphan sessions

## 11) CI Stage Plan (Backend Repo)
Proposed pipeline:
1. `validate`
   - `go fmt` check
   - static lint
   - contract schema validation
2. `test-unit`
   - all `rtc/*` and policy units
3. `test-integration`
   - API/signaling/SFU adapter integration suite
4. `test-e2e-smoke` (nightly + protected branches)
   - multi-client smoke with Postgres + TURN
5. `test-load-chaos` (scheduled)
   - load + soak + fault scenarios with trend reporting

## 12) Release Gates
Minimum recommended gates before enabling RTC by default:
- `join_success_rate >= 99%` in staging smoke runs
- `rtc_negotiation_error_rate <= 1%` at target load profile
- `forced_disconnect_latency_p95 <= 1s` for kick/ban actions
- zero critical/sev1 crash findings in 7-day soak window

## 13) Observability Verification
Assert metrics/logging coverage in tests:
- metrics emitted for join, fail, publish, disconnect
- structured logs contain identifiers (`server_id`, `channel_id`, `user_uid`, `device_id`)
- sensitive fields redacted (tickets, ICE credentials, raw SDP)

## 14) Cross-Repo Coordination
- Backend contract changes require synchronized client fixture updates.
- Signaling `protocol_version` bumps must include migration notes and dual-read period tests if needed.
- Shared golden fixtures should be version-tagged and consumed in both repos.

## 15) Open Questions
- Which participant-count profile should define MVP production readiness?
- Should load tests be mandatory pre-merge for RTC-related PRs or nightly only?
