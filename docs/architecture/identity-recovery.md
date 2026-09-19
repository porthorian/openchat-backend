# Identity binding and operator recovery

Status: rollout procedure, 2026-09-18. Do not use UID headers to establish identity outside explicit local development.

1. Export current server IDs, users, memberships, roles, channel/message IDs, profiles, and read cursors before cutover. Record counts and SHA-256 checksums. Restore the export into a staging Postgres instance and compare every count and ID set. Keep the original service read-only until the import and restart checks pass.
2. Generate a new client Ed25519 signing key in OS-backed storage. New identities derive a UID from its public key. The private key never leaves the client.
3. For each existing UID, a server owner or moderator verifies the claimant out of band using established community records and creates a pending binding for that UID and public key. Approval records approver UID and time in the append-only audit. The claimant then signs a fresh server challenge. A matching UID string alone is not evidence.
4. Bootstrap the first verified owner through a one-time operator recovery ceremony: stop writes, verify the operator's ownership records and target public-key fingerprint independently, insert a pending owner binding using a restricted database role, and record operator identity, ticket, timestamp, server ID, UID, and key fingerprint in the audit log. Require a second operator review before activating the binding. Revoke the temporary database role afterward.
5. Restart with production header authentication disabled, verify signed session issuance and membership history for the owner, then verify a second client cannot claim the same UID. Keep a tested backup and rollback to the read-only original service until the cutover is accepted.

This procedure is a release gate. The schema and contract alone do not authorize a live migration or enable sanctions.
