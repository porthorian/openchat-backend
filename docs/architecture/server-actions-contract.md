# Server actions and moderation contract

Status: implementation contract, 2026-09-18. All IDs are server-scoped; JSON fields use snake_case. Servers advertise a feature only after its storage and enforcement are active. Unsupported actions fail closed with `feature_unsupported`; `401`, `403`, `404`, `409`, and `410` retain their ordinary meanings. Collection reads use opaque cursor pagination.

## Identity and authorization

Protected requests use a server-issued bearer session bound to a signed client identity. The challenge binds nonce, server ID, projected UID, device ID, and expiry. A new UID derives from the public key; legacy UIDs require an audited, out-of-band verified binding approved by an owner or moderator. The first owner requires a documented operator recovery procedure. Caller-supplied UID headers are accepted only in explicit local development mode. Session and role validity are checked on HTTP, realtime, and RTC paths. The database is authoritative for expiry and revocation. `POST /v1/servers/{server_id}/sessions/challenge` accepts a base64url Ed25519 `public_key`, `device_id`, and optional legacy `user_uid` and returns `challenge_id`, `payload`, `user_uid`, and `expires_at`. The client signs the UTF-8 payload. `POST /v1/servers/{server_id}/sessions` accepts the challenge ID and base64url signature and returns a single-use bearer token and expiry. `POST /v1/servers/{server_id}/identity-bindings/{user_uid}:approve` is restricted to owner/moderator after independent out-of-band verification and records a verification reference in the immutable audit. This endpoint is not the first-owner bootstrap path.

## Server actions

- `PUT /v1/servers/{server_id}/read-acks`: monotonic bulk channel read cursors, scoped to the caller; response returns authoritative cursors.
- `GET|POST /v1/servers/{server_id}/invites`, `DELETE /v1/servers/{server_id}/invites/{invite_id}`, `POST /v1/invites/{code}/redeem`: owner/admin creation, list, and revocation; atomic expiry and maximum-use checks. Store a hash of the invite code.
- `GET|PUT /v1/servers/{server_id}/privacy/me`: caller's presence visibility and mention permissions; server enforces both. Read-ack data is never disclosed to other members.
- `GET|PUT|DELETE /v1/servers/{server_id}/profile/me`: optional, explicitly consented display name/avatar override with `If-Match` versioning. Deletion clears the override. No profile attributes are required for baseline messaging.
- `GET|POST /v1/servers/{server_id}/events`, `GET|PUT|DELETE /v1/servers/{server_id}/events/{event_id}`, `POST /v1/servers/{server_id}/events/{event_id}/rsvp`: one-time or daily/weekly/monthly recurring events with IANA time zone, occurrence exceptions, and bounded listing windows. Owner/admin/moderator create; members view and RSVP.
- `GET|PUT /v1/servers/{server_id}/roles`: owner manages role assignments; role changes are audited. Members cannot grant themselves privileges.

Notification policy and channel mute are local client preferences. Existing channel/category and server profile APIs remain in place.

## Moderation

- `GET|PUT /v1/servers/{server_id}/moderation/policy`: owner-only policy update; changes apply to future proposals and are audited. Default: two yes votes, three distinct eligible voters, 24-hour window.
- `GET|POST /v1/servers/{server_id}/moderation/cases`, `GET /v1/servers/{server_id}/moderation/cases/{case_id}`: any member may report; reporter reads own status, target reads actions affecting them, eligible staff read full case data.
- `POST /v1/servers/{server_id}/moderation/cases/{case_id}/actions/immediate`: kick, short timeout, temporary channel lock.
- `POST /v1/servers/{server_id}/moderation/cases/{case_id}/actions/propose`, `POST /v1/servers/{server_id}/moderation/cases/{case_id}/votes`: ban, long timeout, role removal. Insufficient eligible voters blocks the proposal; votes and enforcement are idempotent and transactional.
- `GET /v1/servers/{server_id}/moderation/audit`: append-only, staff-scoped audit metadata.

Owner appoints roles and edits policy. Admins and moderators handle cases and vote. Nobody sanctions an equal or higher role; the owner is protected. Reports include references and a reason. Plaintext evidence is optional, requires separate reporter consent, is encrypted at rest, and is deleted 30 days after case closure; audit metadata remains. No channel encryption rollout is implied. For future protected channels, removal must not resume protected messaging before a successful epoch transition.

Enforcement must deny further HTTP writes, realtime delivery, and RTC join/publish as applicable, revoke affected sessions, and publish server-scoped state changes. Failures in notification fanout do not undo committed restrictions; durable outbox retries delivery.
