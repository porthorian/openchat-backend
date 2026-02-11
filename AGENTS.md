# OpenChat Backend Planning Document

## Purpose
This file defines implementation constraints and planning rules for `openchat-backend`.

## Agent Execution Rules
- Do not run any `git` commands in this repository.
- Do not run commands unless explicitly approved by the user or explicitly allowed in this file.
- Never reintroduce code/content that the user previously removed.
- Prefer targeted edits over full-file rewrites unless the user explicitly requests a rewrite.

## Repository Scope
- In scope:
  - Go backend services for OpenChat.
  - Postgres-backed persistence.
  - Capability discovery and transport/session services.
  - End-to-end encrypted (E2EE) message relay/storage patterns.
- Out of scope:
  - Frontend renderer/UI code.
  - Client key storage implementation details beyond backend interface contracts.

## Security and Privacy Requirements
- Backend must store ciphertext for protected channel content.
- Backend must not require plaintext user profile attributes for messaging.
- Channel history visibility must follow membership-time policy:
  - members present at send time can decrypt
  - later joiners cannot decrypt prior messages
- Membership changes must rotate channel epoch keys (client-managed cryptography, server-managed membership/epoch metadata).

## Architecture Planning Requirements
- Keep contracts explicit and versioned.
- Design around decentralized deployment:
  - one backend instance manages only its own OpenChat server/community data.
- Prefer simple, auditable interfaces before optimization.
- Any change that expands data visibility beyond ciphertext + required metadata requires a documented review.

## Documentation Expectations
- Keep architecture docs under `docs/architecture/`.
- Keep API contracts in docs before implementation.
- Include threat model notes and failure-mode behavior for E2EE-related features.
