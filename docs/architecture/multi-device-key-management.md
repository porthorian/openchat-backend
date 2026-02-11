# Multi-Device Decryption and Key Management

## 1) Problem
A user should read eligible message history on multiple devices (desktop + mobile) while preserving E2EE.

## 2) Core Principle
Membership rights are user-level; decryption secrets are device-delivered.

A newly added device can decrypt old eligible history only after secure key transfer or recovery.

## 3) Device Linking Flow (Primary)
1. New device generates keypair and linking token.
2. Existing trusted device verifies linking request (QR/out-of-band check).
3. Existing device encrypts historical epoch key bundles for new device public key.
4. Backend relays encrypted bundles; new device decrypts locally.

Backend never sees plaintext epoch secrets.

## 4) Encrypted Recovery Backup (Secondary)
- Client creates encrypted key backup blobs using user recovery secret.
- Backend stores encrypted backup blobs only.
- New device restores by presenting recovery secret locally.

## 5) Access Rules
- Device added for existing member can receive keys for epochs where user had membership.
- Device for newly joined user does not receive keys for epochs before user membership start.
- Device revocation blocks future key envelope delivery to that device.

## 6) Data Model (High-Level)
- `user_devices`:
  - `user_uid`, `device_id`, `device_pubkey`, `status`, timestamps
- `device_link_requests`:
  - `request_id`, `user_uid`, `new_device_id`, `state`, `created_at`, `expires_at`
- `epoch_key_envelopes`:
  - `channel_id`, `epoch_id`, `target_device_id`, `encrypted_key_blob`, `created_at`
- `encrypted_key_backups`:
  - `backup_id`, `user_uid`, `ciphertext_blob`, `kdf_metadata`, `created_at`

## 7) Security Notes
- Require explicit user confirmation for device linking.
- Strongly authenticate device linking actions.
- Expire pending link requests quickly.
- Do not log key envelope payloads.
