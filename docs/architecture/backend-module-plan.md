# Go Backend Module and Folder Plan

## 1) Proposed Layout
```text
openchat-backend/
  cmd/
    openchatd/
      main.go
  internal/
    app/
      bootstrap.go
      config.go
    api/
      router.go
      middleware_auth.go
      middleware_rate_limit.go
      handlers_capabilities.go
      handlers_channels.go
      handlers_messages.go
      handlers_membership.go
      handlers_rtc.go
      handlers_moderation.go
      handlers_devices.go
      handlers_keysync.go
    realtime/
      hub.go
      ws_session.go
      events.go
    auth/
      token_service.go
      session_service.go
    membership/
      service.go
      policy.go
    epochs/
      service.go
      commit_validator.go
    messages/
      service.go
      envelope_validator.go
    rtc/
      token_service.go
      signaling_service.go
      room_service.go
      sfu_adapter.go
      media_policy.go
    moderation/
      policy_service.go
      case_service.go
      vote_service.go
      enforcement_service.go
    keysync/
      device_link_service.go
      envelope_service.go
      backup_service.go
    cryptoedge/
      envelope_types.go
      audit_redaction.go
    store/
      postgres/
        db.go
        migrations/
        repo_channels.go
        repo_membership.go
        repo_epochs.go
        repo_messages.go
        repo_rtc.go
        repo_moderation.go
        repo_devices.go
        repo_keysync.go
    capabilities/
      service.go
      schema.go
    observability/
      logging.go
      metrics.go
      tracing.go
  docs/
    architecture/
```

## 2) Service Boundaries
- `capabilities`: returns server feature/capability contract (including RTC/E2EE flags).
- `membership`: authoritative channel membership timeline.
- `epochs`: handles epoch sequence and commit validation.
- `messages`: persists/retrieves encrypted message envelopes.
- `rtc`: join-ticket auth, signaling, SFU room orchestration, and media policy enforcement.
- `moderation`: policy, cases, report bundles, voting, and enforcement decisions.
- `keysync`: relays encrypted key envelopes + manages encrypted backups.
- `realtime`: websocket fanout of encrypted events and membership/epoch updates.

## 3) API Contract Sketch
- `GET /v1/client/capabilities`
- `GET /v1/channels/:channel_id/history?cursor=...`
- `POST /v1/channels/:channel_id/messages`
- `POST /v1/channels/:channel_id/membership/commit`
- `POST /v1/rtc/channels/:channel_id/join-ticket`
- `GET /v1/rtc/signaling` (websocket upgrade)
- `GET /v1/moderation/policy`
- `PUT /v1/moderation/policy`
- `POST /v1/moderation/cases`
- `GET /v1/moderation/cases`
- `POST /v1/moderation/cases/:case_id/actions/immediate`
- `POST /v1/moderation/cases/:case_id/actions/propose`
- `POST /v1/moderation/cases/:case_id/votes`
- `POST /v1/devices/link/start`
- `POST /v1/devices/link/confirm`
- `POST /v1/keys/envelopes/publish`
- `GET /v1/keys/envelopes?device_id=...`
- `POST /v1/keys/backups`
- `GET /v1/keys/backups/latest`

## 4) Postgres Priorities
- Build append-friendly tables for messages and epoch commits.
- Index by `channel_id`, `epoch_id`, and chronological cursor fields.
- Enforce foreign-key integrity for channel/epoch/membership relationships.

## 5) Rollout Order
1. Capabilities + auth/session baseline.
2. Membership + epoch commit chain.
3. Encrypted message ingest/history retrieval.
4. RTC join-ticket, signaling, and SFU voice baseline.
5. RTC video/screenshare + scaling hardening.
6. Moderation policy/case/vote enforcement.
7. Device linking and key envelope relay.
8. Realtime event fanout and operational hardening.
