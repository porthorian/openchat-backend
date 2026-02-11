# Implementation Open Questions (Backend)

Last updated: 2026-02-11

## 1) SFU Engine Choice
- Should we implement SFU directly with Pion primitives or integrate Ion-SFU behind `rtc/sfu_adapter.go` first?
- What level of codec/simulcast control do we require for MVP vs post-MVP?

## 2) Authentication and Session Binding
- Current implementation accepts UID/device headers for join-ticket flow in non-production mode.
- What is the exact production session validation contract (token claims, issuer, expiry, revocation behavior)?

## 3) TURN/STUN Provisioning
- What TURN provider/deployment model should be the default for decentralized operators?
- Should TURN credentials always be ephemeral, and what TTL window should be enforced?

## 4) Room and Scaling Model
- At what participant threshold do we split signaling and SFU into separate worker processes?
- What room-affinity and worker-dispatch strategy should we standardize for horizontal scaling?

## 5) Moderation Enforcement Semantics
- For `timeout`, should backend enforce listen-only in RTC or fully deny room join based on policy?
- Should emergency `kick` require post-hoc moderation vote ratification in policy defaults?

## 6) Persistence and Audit Scope
- Which RTC events should be persisted by default (join/leave only vs richer signaling audit metadata)?
- Do we need retention/TTL policy for RTC audit records at MVP launch?

## 7) Signaling Protocol Versioning
- Should we support dual protocol versions during rollout (`protocol_version` N and N-1), or require strict cutover?
- What explicit deprecation policy should be documented for signaling message types?

## 8) Operational SLOs
- Confirm target SLOs for MVP:
  - join success rate
  - reconnect success rate
  - forced disconnect latency
  - negotiation error budget

## 9) Failure Recovery Behavior
- On backend restart, should we attempt session resume hints or always require full rejoin?
- Do we want bounded grace windows configurable per server profile for reconnect handling?
