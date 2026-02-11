# Moderation and Governance under E2EE

## 1) Problem
OpenChat requires moderation controls for abuse prevention while preserving end-to-end encrypted channel content. Moderators may not always have decryption access to historical messages, so policy and evidence handling cannot assume universal moderator visibility.

## 2) Governance Model
Use a hybrid model:

- Immediate safety actions (`single-moderator`):
  - `kick`
  - short `timeout`
  - temporary `channel_lock`
- Permanent or high-impact actions (`vote-required`):
  - `ban`
  - long `timeout`
  - role-stripping

Rationale:
- Immediate actions reduce active harm quickly.
- Vote-gated actions reduce unilateral abuse risk and create better accountability.

## 3) Policy Object (Server-Scoped)
Each server stores a moderation policy object:

```json
{
  "enabled": true,
  "actions": {
    "immediate": ["kick", "timeout_short", "channel_lock"],
    "vote_required": ["ban", "timeout_long", "role_remove"]
  },
  "vote_policy": {
    "threshold": 2,
    "quorum": 3,
    "window_seconds": 86400
  },
  "evidence_policy": {
    "report_bundle_required": true,
    "plaintext_disclosure_optional": true
  }
}
```

Policy is server-local and can vary by decentralized deployment.

## 4) E2EE Evidence Model
Because moderators may not decrypt all content:
- Cases are based on report bundles submitted by users who could see content.
- Report bundles include:
  - message ids
  - ciphertext metadata references
  - reason/category
  - optional plaintext disclosures intentionally provided by reporter
- Backend never requires plaintext ingestion and stores evidence as opaque payloads when possible.

No report bundle means no guaranteed retrospective content review.

## 5) Moderation Case Lifecycle
1. Report submitted.
2. Backend creates case (`open`).
3. Moderators can:
   - apply immediate action if allowed by policy
   - propose vote-required action
4. Votes collected during `window_seconds`.
5. Backend evaluates `threshold` + `quorum`.
6. Action either:
   - `enforced`
   - `rejected` (insufficient votes/expired)
7. Case transitions to `closed`.

All transitions are written to immutable audit records.

## 6) Enforcement and E2EE Side Effects
- `kick`:
  - remove active membership/session
  - trigger channel epoch rotation for impacted channels
- `ban`:
  - deny future joins/sessions
  - trigger channel epoch rotation and key distribution updates
- `timeout`:
  - server-side posting restrictions without mandatory history deletion

Epoch rotation is required to prevent removed members from decrypting future messages.

## 7) Backend API Sketch
- `GET /v1/moderation/policy`
- `PUT /v1/moderation/policy` (admin only)
- `POST /v1/moderation/cases`
- `GET /v1/moderation/cases`
- `GET /v1/moderation/cases/:case_id`
- `POST /v1/moderation/cases/:case_id/actions/immediate`
- `POST /v1/moderation/cases/:case_id/actions/propose`
- `POST /v1/moderation/cases/:case_id/votes`
- `GET /v1/moderation/cases/:case_id/votes`
- `GET /v1/moderation/audit`

Realtime events should include case status and enforcement outcomes for client updates.

## 8) Postgres Model (High-Level)
- `moderation_policies`:
  - `server_id`, `policy_json`, `updated_by_uid`, `updated_at`
- `moderation_cases`:
  - `case_id`, `server_id`, `target_uid`, `status`, `reason_code`, `created_by_uid`, timestamps
- `moderation_reports`:
  - `report_id`, `case_id`, `reporter_uid`, `cipher_refs_json`, `plaintext_disclosure_blob`, `created_at`
- `moderation_proposals`:
  - `proposal_id`, `case_id`, `action_type`, `duration_seconds`, `created_by_uid`, `created_at`
- `moderation_votes`:
  - `proposal_id`, `voter_uid`, `vote_value`, `created_at`
- `moderation_actions`:
  - `action_id`, `case_id`, `action_type`, `decision`, `enforced_by_uid`, `enforced_at`
- `moderation_audit_log`:
  - append-only case/action/vote event records

## 9) Security and Abuse Resistance
- Enforce strict role checks on all moderation endpoints.
- Require idempotency keys on action endpoints to avoid duplicate enforcement.
- Rate limit report and vote endpoints.
- Redact evidence payload fields from logs.
- Keep appeal workflow metadata separate from primary case decision path.

## 10) Open Questions
- Should emergency actions also require post-hoc vote ratification?
- Should each channel be allowed to override server-level moderation vote policy?
- What minimum moderation audit visibility should non-moderator members receive?
