# WebRTC SFU Backend Design

## 1) Goals
Design a backend media system for decentralized OpenChat servers that provides:
- low-latency voice/video/screen-share in channel sessions
- SFU-first group call scaling
- compatibility with client capability negotiation
- strict UID-only identity boundaries
- operational control for moderation and abuse handling

## 2) Non-Goals
- End-to-end media encryption beyond baseline WebRTC DTLS-SRTP for MVP.
- Media recording/transcoding pipelines.
- Cross-server/federated media routing.

## 3) High-Level Topology
Each OpenChat backend instance owns media for its own server/community.

Core components:
- `rtc token service`: mints short-lived join tickets bound to user/session/channel.
- `signaling gateway` (WSS): exchanges SDP/ICE and call control events.
- `room manager`: authoritative in-memory participant and track state per channel.
- `sfu engine`: routes RTP streams between participants.
- `ice config provider`: serves STUN/TURN config and credential policy.

Initial deployment target:
- single binary/process with API + signaling + SFU together.

Scale-up target:
- split signaling and SFU workers with room affinity and control-plane messaging.

## 4) Backend Technology Direction (Go)
Recommended base stack:
- Go service runtime + Gorilla/WebSocket or equivalent upgraded endpoint for signaling.
- Pion-based SFU layer (native Pion composition or Ion-SFU integration) behind an adapter.
- Coturn-compatible TURN credentials (time-bound) issued by backend policy.

Design rule:
- keep SFU vendor details behind `rtc/sfu_adapter.go` so implementation can swap without API changes.

## 5) Capability Contract (Discovery)
`GET /client/capabilities` should include `rtc` when media is supported:

```json
{
  "rtc": {
    "protocol_version": "1.0",
    "signaling_url": "wss://example.org/v1/rtc/signaling",
    "signaling_transport": "websocket",
    "topologies": ["sfu"],
    "features": {
      "voice": true,
      "video": true,
      "screenshare": true,
      "simulcast": true
    },
    "ice_servers": [
      {
        "urls": ["stun:stun.example.org:3478"]
      },
      {
        "urls": ["turns:turn.example.org:5349"],
        "username": "u123",
        "credential": "secret",
        "credential_type": "ephemeral",
        "expires_at": "2026-02-11T16:00:00Z"
      }
    ],
    "connection_policy": {
      "join_timeout_ms": 12000,
      "answer_timeout_ms": 10000,
      "ice_restart_enabled": true,
      "reconnect_backoff_ms": [250, 500, 1000, 2000, 5000]
    }
  }
}
```

## 6) Auth and Join Flow
1. Client calls `POST /v1/rtc/channels/:channel_id/join-ticket`.
2. Backend verifies:
   - valid session token
   - channel membership
   - permission to connect/speak/stream
   - not kicked/banned/timeout-blocked
3. Backend returns short-lived one-time join ticket and ICE snapshot.
4. Client opens WSS signaling and presents join ticket.
5. Backend validates ticket, joins user to room manager, and starts SDP/ICE negotiation.

Join ticket claims (minimum):
- `server_id`
- `channel_id`
- `user_uid`
- `device_id`
- `permissions` (`speak`, `video`, `screenshare`)
- `exp` (short TTL, e.g. 60 seconds)
- `jti` (single-use id)

## 7) Signaling Protocol (Version 1)
Message envelope:

```json
{
  "type": "rtc.offer.publish",
  "request_id": "req_123",
  "channel_id": "ch_42",
  "payload": {}
}
```

Client -> server messages:
- `rtc.join`
- `rtc.offer.publish`
- `rtc.offer.subscribe`
- `rtc.ice.candidate`
- `rtc.media.state` (mute/deafen/video)
- `rtc.leave`
- `rtc.ping`

Server -> client messages:
- `rtc.joined`
- `rtc.answer.publish`
- `rtc.answer.subscribe`
- `rtc.ice.candidate`
- `rtc.participant.joined`
- `rtc.participant.left`
- `rtc.track.published`
- `rtc.track.unpublished`
- `rtc.speaking`
- `rtc.kicked`
- `rtc.error`
- `rtc.pong`

Protocol requirements:
- include `request_id` echo for request/response correlation
- idempotent handling for retransmitted client messages
- explicit error codes for permission/token/negotiation failures

## 8) Peer Connection Strategy
Use dual-peer model per participant:
- one publisher peer connection (client -> SFU)
- one subscriber peer connection (SFU -> client)

Why:
- simpler renegotiation under many remote track changes
- cleaner permission enforcement per direction
- better fault isolation for publish vs subscribe failures

## 9) Room State and Data Handling
Room state is primarily ephemeral/in-memory:
- active participants
- published tracks and source metadata
- speaking state and mute/deafen flags

Persist only operational metadata needed for auditing/troubleshooting:
- join/leave timestamps
- moderation-related disconnect reasons
- aggregate QoS counters (not raw media)

Never persist:
- RTP payloads
- decoded media frames
- raw SDP beyond short-lived debugging snapshots (if explicitly enabled)

## 10) Moderation and Governance Integration
RTC must honor moderation policy in real time:
- `kick`: immediate room eviction and signaling disconnect
- `ban`: deny ticket issuance and disconnect active sessions
- `timeout`: deny publish rights (voice/video) while allowing listen-only if policy permits
- `channel_lock`: deny new join tickets except exempt roles

All enforcement actions should emit realtime moderation + rtc events for client state sync.

## 11) ICE/TURN and Network Policy
- Prefer TURN over TLS (`turns`) for restrictive enterprise/home NAT environments.
- Support ephemeral TURN credentials with strict expiry.
- Allow deployment-specific STUN/TURN pool configuration per server instance.
- Reject insecure signaling transport in production mode.

## 12) Abuse Protection and Limits
- Rate limit join-ticket minting and signaling message throughput.
- Enforce max participants per channel from policy/capabilities.
- Limit publish track count per participant.
- Apply backpressure and eviction policy for overloaded rooms.

## 13) Observability
Metrics:
- `rtc_active_rooms`
- `rtc_active_participants`
- `rtc_join_ticket_issued_total`
- `rtc_join_fail_total`
- `rtc_publish_track_total`
- `rtc_negotiation_error_total`
- `rtc_forced_disconnect_total`

Structured logs:
- include `server_id`, `channel_id`, `user_uid`, `device_id`, `session_id`
- redact ICE credentials, tokens, and SDP secrets

## 14) Failure and Recovery Behavior
- On signaling disconnect, room manager marks participant stale and starts grace timer.
- If reconnect succeeds within grace window, resume subscriber state where possible.
- If not, fully remove participant and broadcast leave.
- On backend restart, all RTC sessions reset; clients rejoin through standard reconnect flow.

## 15) Rollout Plan
1. Phase A: capability contract + join-ticket endpoint + signaling skeleton.
2. Phase B: SFU publish/subscribe with voice only.
3. Phase C: video + simulcast + speaking indicators.
4. Phase D: screenshare and hardening (limits, abuse controls, metrics).
5. Phase E: horizontal scaling model (room affinity + multi-worker routing).

## 16) Open Questions
- Should `p2p` be advertised for 1:1 calls when SFU capacity is constrained?
- Which baseline codec profile is required for first stable release (`VP8/H264/Opus` mix)?
- Do we need an explicit server-side SFU failover handshake for long-lived calls?
