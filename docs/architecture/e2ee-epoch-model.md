# E2EE Epoch Model (Channel Message Boards)

## 1) Problem
We need encrypted channel message boards where:
- server operators cannot read plaintext
- outside parties cannot read plaintext
- only users who were channel members when a message was sent can decrypt it
- users joining later cannot decrypt earlier channel history

## 2) Core Decision
Use per-channel epoch-based group encryption (MLS-style design goals).

- Each channel has ordered epochs: `epoch_1`, `epoch_2`, ...
- Every membership change (join/leave/remove/ban) creates a new epoch.
- Message encryption keys derive from the active epoch secret.
- New members only receive current/future epoch key material.
- Old epoch keys are never shared with newly joined users.

## 3) Trust Boundary
- Client devices own content decryption keys.
- Backend stores only encrypted payloads and membership/epoch metadata.
- Backend enforces membership policy for retrieval and posting authorization.
- Backend cannot decrypt message bodies.

## 4) Entities
- `user_uid`: opaque user identifier.
- `device_id`: per-device identifier bound to a device keypair.
- `channel_id`: logical message board.
- `epoch_id`: monotonic channel key epoch.
- `membership_event`: join/leave/remove event with signer and timestamp.
- `epoch_commit`: signed metadata describing epoch transition.

## 5) Message Envelope (Stored by Backend)
```json
{
  "channel_id": "ch_123",
  "epoch_id": 42,
  "message_id": "msg_abc",
  "sender_user_uid": "uid_x",
  "sender_device_id": "dev_a",
  "ciphertext": "base64...",
  "nonce": "base64...",
  "aad": "base64...",
  "created_at": "2026-02-11T00:00:00Z"
}
```

## 6) Membership and Epoch Flow
1. Member set changes (join/leave/remove).
2. Channel epoch rotates to `epoch_n+1`.
3. Active members receive wrapped key material for new epoch.
4. Messages after rotation must use the new epoch.

Consequence:
- New joiners cannot decrypt older ciphertext from prior epochs.

## 7) Backend Policy Enforcement
- Posting:
  - accept message only if sender is active member for supplied `epoch_id`.
- History retrieval:
  - return only messages in epochs the requester was a member of.
  - this limits ciphertext exposure and bandwidth.
- Epoch commits:
  - persist commit chain and reject invalid sequence transitions.

## 8) Threat Model Notes
Mitigated:
- server plaintext disclosure
- storage compromise exposing plaintext

Not fully mitigated:
- metadata leakage (activity patterns, message sizes, timestamps)
- malicious legitimate member exfiltration (copy/screenshot)

## 9) Postgres Data Model (High-Level)
- `channel_memberships`:
  - `channel_id`, `user_uid`, `joined_epoch`, `left_epoch`, timestamps
- `channel_epochs`:
  - `channel_id`, `epoch_id`, `commit_payload`, `commit_signature`, `created_at`
- `channel_messages`:
  - `channel_id`, `epoch_id`, `message_id`, `sender_user_uid`, `sender_device_id`, `ciphertext`, `nonce`, `aad`, `created_at`

## 10) Operational Constraints
- Enforce monotonic `epoch_id` per channel.
- Maintain immutable message/event auditability (append-only semantics).
- Keep crypto material out of logs.
