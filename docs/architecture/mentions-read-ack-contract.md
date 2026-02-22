# Mentions and Read Ack Contract

## 1) Problem
OpenChat needs mention-aware notifications and unread indicators that remain accurate across reconnects and read state changes. The backend must support this without weakening E2EE constraints where message bodies are ciphertext and not parsed server-side.

## 2) Goals and Non-Goals
Goals:
- Define explicit capability fields for mention and read-ack support.
- Define API contracts for mention resolution and read-ack state.
- Define realtime event metadata required for dedupe and mention notifications.
- Keep mention behavior server-scoped and privacy-preserving.

Non-goals:
- Full notification center implementation.
- Cross-server/global mention routing.
- Backend plaintext parsing of encrypted message bodies.

## 3) Capability Discovery Contract
`GET /v1/client/capabilities` should advertise mention and read-ack behavior explicitly.

Example shape (illustrative):
```json
{
  "features": {
    "mentions": {
      "user": true,
      "channel": true,
      "resolve": true,
      "notifications": true,
      "supported_tokens": ["@here", "@channel"]
    },
    "read_acks": {
      "channel": true,
      "cursor_type": "message_id",
      "monotonic": true
    }
  }
}
```

Contract rules:
- Mention/read-ack features are fail-closed: missing fields imply unsupported behavior.
- `supported_tokens` is additive and can include server-specific audience mention tokens.
- `read_acks.channel=true` is required for accurate mention counter clearing.

## 4) Message and Event Metadata Contract
Because channel payload content can be ciphertext, mention metadata is provided as explicit structured metadata in write and realtime envelopes.

Message-level mention metadata fields:
- `mention_type`: `user` or `channel`
- `token`: raw token string (for example `@here`)
- `target_id`: stable `user_uid` for user mentions when applicable
- `display_text`: render-safe text label
- `range`: optional client render offsets

Realtime event metadata fields (minimum):
- `event_id` (stable, replay-safe)
- `server_id`
- `channel_id`
- `message_id`
- `created_at`
- `mentions[]` (normalized mention metadata)

Deduplication expectation:
- Event IDs are stable across reconnect replay.
- Duplicate fanout must not cause duplicate notify decisions for the same mention class.

## 5) Mention Resolve API Contract
Endpoint:
- `GET /v1/channels/:channel_id/mentions:resolve?query=...`

Behavior:
- Returns mention candidates visible to requester for that channel.
- Supports both user mention candidates and special audience tokens (`@here`, `@channel`, server-defined).
- Must enforce membership and permission checks before returning candidates.

Response shape (illustrative):
```json
{
  "results": [
    {
      "type": "user",
      "target_id": "uid_123",
      "display_text": "alex"
    },
    {
      "type": "channel",
      "token": "@here",
      "display_text": "here"
    }
  ]
}
```

Privacy constraints:
- Do not require or return profile attributes beyond what is needed for mention selection.
- Default to opaque IDs and minimal display labels.

## 6) Read Ack API Contract
Endpoints:
- `PUT /v1/channels/:channel_id/read-ack`
- `GET /v1/channels/:channel_id/read-ack`

Write request (illustrative):
```json
{
  "last_read_message_id": "msg_456",
  "acked_at": "2026-02-22T00:00:00Z"
}
```

Behavior rules:
- Read ack cursor is monotonic per `(user_uid, channel_id)`.
- Older/stale acks are ignored (idempotent no-op).
- Ack updates must be replay-safe and safe under reconnect retries.
- Read ack is authoritative for clearing mention counters in clients.

## 7) Notification Semantics
- User mentions (`type=user` targeting requester UID) are highest priority.
- Audience mentions (`@here`, `@channel`, and server-defined audience tokens) are lower priority and policy-gated.
- Backend should include enough metadata for client suppression rules (mute/focus/privacy) to run deterministically.
- Backend may optionally emit normalized mention class in events to simplify client policy logic.

## 8) Persistence and Query Model (High Level)
Suggested tables/repositories:
- `channel_read_acks`: requester UID + channel ID + last read cursor + updated timestamp.
- `message_mentions`: message ID + channel ID + mention type + target/token (normalized query support).

Operational requirements:
- Index `channel_read_acks` by `(channel_id, user_uid)`.
- Index `message_mentions` by `(channel_id, target_id)` and `(channel_id, token)`.
- Keep retention/TTL policy explicit if mention metadata is treated as ephemeral.

## 9) Security and Privacy Notes
- Preserve E2EE boundary: backend processes mention metadata only, not plaintext message bodies.
- Mention metadata is potentially sensitive social graph data; minimize fields and log redaction.
- Do not expand identity disclosure requirements beyond UID/protocol proof baseline.
- All mention/read-ack state is server-local and must never leak across federated peers.

## 10) Failure Modes and Degraded Behavior
- If `mentions.resolve` is unavailable, clients can fall back to raw `@text` entry.
- If mention metadata is malformed, treat as non-mention text and avoid notify side effects.
- If read-ack service is degraded, client counters may drift; capability/status should surface degraded-state warnings.
- On restart/replay, event IDs and read-ack monotonic checks prevent repeated notification side effects.

## 11) Open Questions
- Should audience mention permissions be role-gated server-side in MVP?
- Should read-ack writes be single-cursor only or support batch channel updates?
- What retention policy should apply to `message_mentions` when source messages age out?
