-- Additive, forward-only schema for verified sessions, server actions, and moderation.
ALTER TABLE auth_identity_bindings DROP CONSTRAINT IF EXISTS auth_identity_bindings_provider_check;
ALTER TABLE auth_identity_bindings ADD CONSTRAINT auth_identity_bindings_provider_check CHECK (provider IN ('dev_header', 'atproto', 'ed25519'));
ALTER TABLE auth_sessions DROP CONSTRAINT IF EXISTS auth_sessions_provider_check;
ALTER TABLE auth_sessions ADD CONSTRAINT auth_sessions_provider_check CHECK (provider IN ('dev_header', 'atproto', 'ed25519'));

CREATE TABLE identity_challenges (
  challenge_id text PRIMARY KEY DEFAULT openchat_generate_id('chal_'),
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  user_uid text NOT NULL,
  device_id text NOT NULL,
  nonce bytea NOT NULL UNIQUE,
  public_key bytea NOT NULL CHECK (octet_length(public_key) = 32),
  signed_payload text NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_identity_challenges_expiry ON identity_challenges (expires_at);
ALTER TABLE auth_sessions ADD COLUMN server_id text REFERENCES servers(server_id) ON DELETE CASCADE;
CREATE UNIQUE INDEX idx_auth_sessions_token_hash ON auth_sessions (token_hash);
CREATE INDEX idx_auth_sessions_server_user ON auth_sessions (server_id, user_uid) WHERE revoked_at IS NULL;

CREATE TABLE legacy_identity_bindings (
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  public_key bytea NOT NULL,
  requested_at timestamptz NOT NULL DEFAULT now(),
  approved_by_uid text REFERENCES users(user_uid) ON DELETE SET NULL,
  verified_at timestamptz,
  PRIMARY KEY (server_id, user_uid),
  UNIQUE (server_id, public_key)
);

CREATE TABLE server_invites (
  invite_id text PRIMARY KEY DEFAULT openchat_generate_id('inv_'),
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  code_hash bytea NOT NULL UNIQUE,
  created_by_uid text NOT NULL REFERENCES users(user_uid),
  expires_at timestamptz NOT NULL,
  max_uses integer NOT NULL CHECK (max_uses > 0),
  use_count integer NOT NULL DEFAULT 0 CHECK (use_count >= 0),
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (use_count <= max_uses)
);
CREATE INDEX idx_server_invites_server ON server_invites (server_id, created_at DESC);

CREATE TABLE server_privacy_preferences (
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  presence_visibility text NOT NULL DEFAULT 'visible' CHECK (presence_visibility IN ('visible', 'hidden')),
  mention_permission text NOT NULL DEFAULT 'all_members' CHECK (mention_permission IN ('all_members', 'staff_only', 'nobody')),
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (server_id, user_uid)
);

CREATE TABLE server_profile_overrides (
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  display_name text NOT NULL,
  avatar_mode text NOT NULL CHECK (avatar_mode IN ('generated', 'uploaded')),
  avatar_preset_id text,
  avatar_asset_id text REFERENCES profile_avatar_assets(avatar_asset_id) ON DELETE SET NULL,
  profile_version integer NOT NULL DEFAULT 1 CHECK (profile_version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (server_id, user_uid),
  CHECK (length(trim(display_name)) BETWEEN 1 AND 100)
);

CREATE TABLE server_events (
  event_id text PRIMARY KEY DEFAULT openchat_generate_id('evt_'),
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  channel_id text REFERENCES channels(channel_id) ON DELETE SET NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  timezone text NOT NULL,
  starts_local timestamp without time zone NOT NULL,
  duration_seconds integer NOT NULL CHECK (duration_seconds BETWEEN 60 AND 604800),
  frequency text NOT NULL DEFAULT 'none' CHECK (frequency IN ('none', 'daily', 'weekly', 'monthly')),
  repeat_interval integer NOT NULL DEFAULT 1 CHECK (repeat_interval BETWEEN 1 AND 52),
  weekdays smallint[] NOT NULL DEFAULT '{}'::smallint[],
  repeat_until_local timestamp without time zone,
  created_by_uid text NOT NULL REFERENCES users(user_uid),
  cancelled_at timestamptz,
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (length(trim(title)) BETWEEN 1 AND 160)
);
CREATE INDEX idx_server_events_server ON server_events (server_id, starts_local);

CREATE TABLE server_event_exceptions (
  event_id text NOT NULL REFERENCES server_events(event_id) ON DELETE CASCADE,
  original_starts_local timestamp without time zone NOT NULL,
  replacement_starts_local timestamp without time zone,
  replacement_duration_seconds integer,
  cancelled boolean NOT NULL DEFAULT false,
  updated_by_uid text NOT NULL REFERENCES users(user_uid),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, original_starts_local)
);
CREATE TABLE server_event_rsvps (
  event_id text NOT NULL REFERENCES server_events(event_id) ON DELETE CASCADE,
  occurrence_starts_local timestamp without time zone NOT NULL,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  response text NOT NULL CHECK (response IN ('going', 'interested', 'declined')),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, occurrence_starts_local, user_uid)
);

CREATE TABLE moderation_policies (
  server_id text PRIMARY KEY REFERENCES servers(server_id) ON DELETE CASCADE,
  threshold integer NOT NULL DEFAULT 2 CHECK (threshold >= 2),
  quorum integer NOT NULL DEFAULT 3 CHECK (quorum >= threshold),
  window_seconds integer NOT NULL DEFAULT 86400 CHECK (window_seconds BETWEEN 300 AND 604800),
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  updated_by_uid text REFERENCES users(user_uid),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE moderation_cases (
  case_id text PRIMARY KEY DEFAULT openchat_generate_id('case_'),
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  reporter_uid text NOT NULL REFERENCES users(user_uid),
  target_uid text NOT NULL REFERENCES users(user_uid),
  channel_id text REFERENCES channels(channel_id) ON DELETE SET NULL,
  message_id text REFERENCES messages(message_id) ON DELETE SET NULL,
  reason_code text NOT NULL,
  reason_text text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'voting', 'enforced', 'rejected', 'closed')),
  created_at timestamptz NOT NULL DEFAULT now(),
  closed_at timestamptz
);
CREATE INDEX idx_moderation_cases_server ON moderation_cases (server_id, created_at DESC, case_id DESC);
CREATE INDEX idx_moderation_cases_reporter ON moderation_cases (reporter_uid, created_at DESC);
CREATE TABLE moderation_report_evidence (
  case_id text PRIMARY KEY REFERENCES moderation_cases(case_id) ON DELETE CASCADE,
  consented_at timestamptz NOT NULL,
  ciphertext bytea NOT NULL,
  nonce bytea NOT NULL,
  key_id text NOT NULL,
  delete_after timestamptz
);
CREATE TABLE moderation_proposals (
  proposal_id text PRIMARY KEY DEFAULT openchat_generate_id('prop_'),
  case_id text NOT NULL REFERENCES moderation_cases(case_id) ON DELETE CASCADE,
  action_type text NOT NULL CHECK (action_type IN ('ban', 'timeout_long', 'role_remove')),
  duration_seconds integer,
  proposed_by_uid text NOT NULL REFERENCES users(user_uid),
  threshold integer NOT NULL CHECK (threshold >= 2),
  quorum integer NOT NULL CHECK (quorum >= threshold),
  expires_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'enforced', 'rejected', 'expired')),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_moderation_one_open_proposal ON moderation_proposals (case_id) WHERE status = 'open';
CREATE TABLE moderation_votes (
  proposal_id text NOT NULL REFERENCES moderation_proposals(proposal_id) ON DELETE CASCADE,
  voter_uid text NOT NULL REFERENCES users(user_uid),
  value text NOT NULL CHECK (value IN ('yes', 'no', 'abstain')),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (proposal_id, voter_uid)
);
CREATE TABLE moderation_actions (
  action_id text PRIMARY KEY DEFAULT openchat_generate_id('act_'),
  case_id text NOT NULL REFERENCES moderation_cases(case_id) ON DELETE CASCADE,
  action_type text NOT NULL CHECK (action_type IN ('kick', 'timeout_short', 'channel_lock', 'ban', 'timeout_long', 'role_remove')),
  target_uid text REFERENCES users(user_uid),
  channel_id text REFERENCES channels(channel_id),
  duration_seconds integer,
  expires_at timestamptz,
  enforced_by_uid text NOT NULL REFERENCES users(user_uid),
  idempotency_key text NOT NULL,
  enforced_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (case_id, idempotency_key)
);
CREATE INDEX idx_moderation_actions_target ON moderation_actions (target_uid, expires_at);
CREATE TABLE moderation_audit (
  audit_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  case_id text REFERENCES moderation_cases(case_id) ON DELETE SET NULL,
  actor_uid text REFERENCES users(user_uid) ON DELETE SET NULL,
  event_type text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_moderation_audit_server ON moderation_audit (server_id, audit_id DESC);
CREATE FUNCTION openchat_reject_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'moderation audit records are immutable';
END;
$$;
CREATE TRIGGER trg_moderation_audit_immutable BEFORE UPDATE OR DELETE ON moderation_audit
  FOR EACH ROW EXECUTE FUNCTION openchat_reject_audit_mutation();
CREATE TABLE server_event_outbox (
  outbox_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  event_type text NOT NULL,
  payload jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz,
  attempts integer NOT NULL DEFAULT 0
);
CREATE INDEX idx_server_event_outbox_pending ON server_event_outbox (outbox_id) WHERE delivered_at IS NULL;
