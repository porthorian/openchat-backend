# Search Model Under E2EE Constraints

## 1) Goal
Provide message search without leaking plaintext message content or query terms to backend operators.

## 2) Core Decision
Use client-side search indexing only.

- Backend does not process plaintext search queries.
- Backend does not build server-side keyword indexes over protected content.
- Client decrypts eligible history and indexes locally.

## 3) Data Flow
1. Client fetches encrypted history for allowed channel epochs.
2. Client decrypts locally using device-held keys.
3. Client writes plaintext index to local encrypted storage.
4. User search executes entirely on-device.

## 4) Privacy Properties
- Query terms are never sent to backend.
- Backend sees request metadata only (history pagination calls).
- Users cannot search content they cannot decrypt.

## 5) UX/Performance Notes
- First search may require initial history sync + local indexing.
- Reindexing occurs incrementally as new decryptable messages arrive.
- New joiners do not index prior-epoch history because they cannot decrypt it.

## 6) Backend Interface Requirements
- Cursor-based history fetch by `channel_id` and time/window.
- Membership-aware filtering for retrievable epochs.
- Stable ordering and idempotent pagination.

## 7) Optional Future Work
- Private information retrieval/searchable encryption is out of scope for MVP due complexity/risk.
