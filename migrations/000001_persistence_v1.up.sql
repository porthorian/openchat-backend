CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION openchat_generate_id(prefix text)
RETURNS text
LANGUAGE sql
AS $$
SELECT prefix || substr(replace(gen_random_uuid()::text, '-', ''), 1, 20);
$$;

CREATE OR REPLACE FUNCTION openchat_set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$;

CREATE TABLE users (
  user_uid text PRIMARY KEY,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (length(trim(user_uid)) > 0)
);

CREATE TABLE servers (
  server_id text PRIMARY KEY DEFAULT openchat_generate_id('srv_'),
  display_name text NOT NULL,
  icon_text text NOT NULL DEFAULT '',
  icon_url text,
  trust_state text NOT NULL DEFAULT 'unverified' CHECK (trust_state IN ('verified', 'unverified', 'warning')),
  identity_handshake_strategy text NOT NULL DEFAULT 'challenge_signature',
  user_identifier_policy text NOT NULL DEFAULT 'server_scoped',
  created_by_user_uid text REFERENCES users(user_uid) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CHECK (length(trim(display_name)) > 0)
);

CREATE TABLE server_memberships (
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  role text NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'moderator', 'member')),
  membership_state text NOT NULL DEFAULT 'active' CHECK (membership_state IN ('active', 'left', 'removed', 'banned')),
  joined_at timestamptz NOT NULL DEFAULT now(),
  left_at timestamptz,
  removed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (server_id, user_uid)
);

CREATE TABLE channel_groups (
  channel_group_id text PRIMARY KEY DEFAULT openchat_generate_id('grp_'),
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  label text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('text', 'voice')),
  position integer NOT NULL DEFAULT 0 CHECK (position >= 0),
  created_by_user_uid text REFERENCES users(user_uid) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CHECK (length(trim(label)) > 0)
);

CREATE TABLE channels (
  channel_id text PRIMARY KEY DEFAULT openchat_generate_id('ch_'),
  server_id text NOT NULL REFERENCES servers(server_id) ON DELETE CASCADE,
  channel_group_id text REFERENCES channel_groups(channel_group_id) ON DELETE SET NULL,
  name text NOT NULL,
  channel_type text NOT NULL CHECK (channel_type IN ('text', 'voice')),
  topic text,
  position integer NOT NULL DEFAULT 0 CHECK (position >= 0),
  created_by_user_uid text REFERENCES users(user_uid) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CONSTRAINT channels_server_channel_unique UNIQUE (server_id, channel_id),
  CHECK (length(trim(name)) > 0)
);

CREATE TABLE channel_memberships (
  channel_id text NOT NULL REFERENCES channels(channel_id) ON DELETE CASCADE,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  membership_state text NOT NULL DEFAULT 'active' CHECK (membership_state IN ('active', 'left', 'removed', 'banned')),
  joined_at timestamptz NOT NULL DEFAULT now(),
  left_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (channel_id, user_uid)
);

CREATE TABLE messages (
  message_id text PRIMARY KEY DEFAULT openchat_generate_id('msg_'),
  server_id text NOT NULL,
  channel_id text NOT NULL,
  author_user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE RESTRICT,
  body_text text,
  encryption_mode text NOT NULL DEFAULT 'plaintext' CHECK (encryption_mode IN ('plaintext', 'e2ee_envelope')),
  ciphertext bytea,
  nonce bytea,
  aad bytea,
  epoch_id bigint,
  reply_to_message_id text REFERENCES messages(message_id) ON DELETE SET NULL,
  idempotency_key text,
  created_at timestamptz NOT NULL DEFAULT now(),
  edited_at timestamptz,
  deleted_at timestamptz,
  CONSTRAINT messages_channel_fk
    FOREIGN KEY (server_id, channel_id)
    REFERENCES channels(server_id, channel_id)
    ON DELETE CASCADE,
  CONSTRAINT messages_payload_check CHECK (
    (
      encryption_mode = 'plaintext'
      AND body_text IS NOT NULL
      AND ciphertext IS NULL
      AND nonce IS NULL
      AND epoch_id IS NULL
    )
    OR
    (
      encryption_mode = 'e2ee_envelope'
      AND body_text IS NULL
      AND ciphertext IS NOT NULL
      AND nonce IS NOT NULL
    )
  )
);

CREATE TABLE message_mentions (
  mention_id bigserial PRIMARY KEY,
  message_id text NOT NULL REFERENCES messages(message_id) ON DELETE CASCADE,
  server_id text NOT NULL,
  channel_id text NOT NULL,
  mention_type text NOT NULL CHECK (mention_type IN ('user', 'channel')),
  token text,
  target_user_uid text REFERENCES users(user_uid) ON DELETE SET NULL,
  target_id text,
  display_text text NOT NULL DEFAULT '',
  range_start integer,
  range_end integer,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT message_mentions_channel_fk
    FOREIGN KEY (server_id, channel_id)
    REFERENCES channels(server_id, channel_id)
    ON DELETE CASCADE,
  CONSTRAINT message_mentions_kind_check CHECK (
    (mention_type = 'user' AND target_user_uid IS NOT NULL)
    OR
    (mention_type = 'channel' AND token IS NOT NULL)
  ),
  CONSTRAINT message_mentions_range_check CHECK (
    (range_start IS NULL AND range_end IS NULL)
    OR
    (range_start >= 0 AND range_end > range_start)
  )
);

CREATE TABLE message_attachments (
  attachment_id text PRIMARY KEY DEFAULT openchat_generate_id('att_'),
  message_id text NOT NULL REFERENCES messages(message_id) ON DELETE CASCADE,
  server_id text NOT NULL,
  channel_id text NOT NULL,
  file_name text NOT NULL,
  content_type text NOT NULL,
  byte_size integer NOT NULL CHECK (byte_size > 0),
  width integer NOT NULL CHECK (width > 0),
  height integer NOT NULL CHECK (height > 0),
  storage_key text NOT NULL,
  public_url text,
  sha256_digest bytea,
  created_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CONSTRAINT message_attachments_channel_fk
    FOREIGN KEY (server_id, channel_id)
    REFERENCES channels(server_id, channel_id)
    ON DELETE CASCADE
);

CREATE TABLE message_attachment_blobs (
  attachment_id text PRIMARY KEY REFERENCES message_attachments(attachment_id) ON DELETE CASCADE,
  content bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE channel_read_acks (
  server_id text NOT NULL,
  channel_id text NOT NULL,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  last_read_message_id text REFERENCES messages(message_id) ON DELETE SET NULL,
  cursor_index integer,
  acked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (channel_id, user_uid),
  CONSTRAINT read_acks_channel_fk
    FOREIGN KEY (server_id, channel_id)
    REFERENCES channels(server_id, channel_id)
    ON DELETE CASCADE,
  CONSTRAINT read_acks_cursor_nonnegative CHECK (cursor_index IS NULL OR cursor_index >= 0)
);

CREATE TABLE profile_avatar_assets (
  avatar_asset_id text PRIMARY KEY DEFAULT openchat_generate_id('asset_'),
  owner_user_uid text REFERENCES users(user_uid) ON DELETE SET NULL,
  content_type text NOT NULL,
  byte_size integer NOT NULL CHECK (byte_size > 0),
  width integer NOT NULL CHECK (width > 0),
  height integer NOT NULL CHECK (height > 0),
  storage_key text NOT NULL,
  public_url text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);

CREATE TABLE profile_avatar_blobs (
  avatar_asset_id text PRIMARY KEY REFERENCES profile_avatar_assets(avatar_asset_id) ON DELETE CASCADE,
  content bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE profiles (
  user_uid text PRIMARY KEY REFERENCES users(user_uid) ON DELETE CASCADE,
  display_name text NOT NULL,
  avatar_mode text NOT NULL DEFAULT 'generated' CHECK (avatar_mode IN ('generated', 'uploaded')),
  avatar_preset_id text,
  avatar_asset_id text REFERENCES profile_avatar_assets(avatar_asset_id) ON DELETE SET NULL,
  profile_version integer NOT NULL DEFAULT 1 CHECK (profile_version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT profiles_avatar_mode_check CHECK (
    (
      avatar_mode = 'generated'
      AND avatar_preset_id IS NOT NULL
      AND avatar_asset_id IS NULL
    )
    OR
    (
      avatar_mode = 'uploaded'
      AND avatar_asset_id IS NOT NULL
      AND avatar_preset_id IS NULL
    )
  )
);

CREATE TABLE auth_identity_bindings (
  binding_id bigserial PRIMARY KEY,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  provider text NOT NULL CHECK (provider IN ('dev_header', 'atproto')),
  provider_subject text NOT NULL,
  provider_claims jsonb NOT NULL DEFAULT '{}'::jsonb,
  is_primary boolean NOT NULL DEFAULT false,
  verified_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, provider_subject),
  UNIQUE (user_uid, provider, provider_subject)
);

CREATE TABLE auth_sessions (
  session_id text PRIMARY KEY DEFAULT openchat_generate_id('sess_'),
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  provider text NOT NULL CHECK (provider IN ('dev_header', 'atproto')),
  token_hash bytea NOT NULL,
  refresh_token_hash bytea,
  scopes text[] NOT NULL DEFAULT '{}'::text[],
  device_id text,
  client_info jsonb NOT NULL DEFAULT '{}'::jsonb,
  issued_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > issued_at)
);

CREATE TABLE atproto_auth_requests (
  request_id text PRIMARY KEY DEFAULT openchat_generate_id('atr_'),
  state text NOT NULL UNIQUE,
  pkce_verifier_hash bytea NOT NULL,
  did_hint text,
  authorization_server text,
  resource_server text,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'failed', 'expired')),
  error_code text,
  error_description text,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  completed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > created_at)
);

CREATE TABLE atproto_authorizations (
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  did text NOT NULL,
  pds_url text,
  access_token_hash bytea,
  refresh_token_hash bytea,
  scopes text[] NOT NULL DEFAULT '{}'::text[],
  token_expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_uid, did)
);

CREATE TABLE rtc_join_ticket_replay (
  jti text PRIMARY KEY,
  server_id text NOT NULL,
  channel_id text NOT NULL,
  user_uid text NOT NULL REFERENCES users(user_uid) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT rtc_ticket_channel_fk
    FOREIGN KEY (server_id, channel_id)
    REFERENCES channels(server_id, channel_id)
    ON DELETE CASCADE
);

CREATE INDEX idx_server_memberships_user_state ON server_memberships (user_uid, membership_state);
CREATE INDEX idx_channel_groups_server_position ON channel_groups (server_id, kind, position) WHERE deleted_at IS NULL;
CREATE INDEX idx_channels_server_position ON channels (server_id, channel_type, position) WHERE deleted_at IS NULL;
CREATE INDEX idx_channel_memberships_user_state ON channel_memberships (user_uid, membership_state);

CREATE INDEX idx_messages_channel_created_desc ON messages (channel_id, created_at DESC, message_id DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_messages_server_created_desc ON messages (server_id, created_at DESC, message_id DESC) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_messages_channel_idempotency_unique ON messages (channel_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_messages_reply_to ON messages (reply_to_message_id) WHERE reply_to_message_id IS NOT NULL;

CREATE INDEX idx_mentions_message ON message_mentions (message_id);
CREATE INDEX idx_mentions_channel_user_target ON message_mentions (channel_id, target_user_uid) WHERE mention_type = 'user';
CREATE INDEX idx_mentions_channel_token ON message_mentions (channel_id, token) WHERE mention_type = 'channel';

CREATE INDEX idx_attachments_message ON message_attachments (message_id);
CREATE INDEX idx_attachments_channel_created ON message_attachments (channel_id, created_at DESC);

CREATE INDEX idx_read_acks_user_channel ON channel_read_acks (user_uid, channel_id);
CREATE INDEX idx_profiles_updated ON profiles (updated_at DESC);

CREATE UNIQUE INDEX idx_auth_identity_primary_per_provider ON auth_identity_bindings (user_uid, provider) WHERE is_primary;
CREATE INDEX idx_auth_sessions_user_active ON auth_sessions (user_uid, expires_at) WHERE revoked_at IS NULL;
CREATE INDEX idx_auth_sessions_expiry ON auth_sessions (expires_at);

CREATE INDEX idx_atproto_auth_requests_state_status ON atproto_auth_requests (state, status);
CREATE INDEX idx_atproto_authorizations_did ON atproto_authorizations (did);

CREATE INDEX idx_rtc_ticket_replay_expiry ON rtc_join_ticket_replay (expires_at);

CREATE TRIGGER trg_users_updated_at
  BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_servers_updated_at
  BEFORE UPDATE ON servers
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_server_memberships_updated_at
  BEFORE UPDATE ON server_memberships
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_channel_groups_updated_at
  BEFORE UPDATE ON channel_groups
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_channels_updated_at
  BEFORE UPDATE ON channels
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_channel_memberships_updated_at
  BEFORE UPDATE ON channel_memberships
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_channel_read_acks_updated_at
  BEFORE UPDATE ON channel_read_acks
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_profile_avatar_assets_updated_at
  BEFORE UPDATE ON profile_avatar_assets
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_profiles_updated_at
  BEFORE UPDATE ON profiles
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_auth_identity_bindings_updated_at
  BEFORE UPDATE ON auth_identity_bindings
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_auth_sessions_updated_at
  BEFORE UPDATE ON auth_sessions
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_atproto_auth_requests_updated_at
  BEFORE UPDATE ON atproto_auth_requests
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();

CREATE TRIGGER trg_atproto_authorizations_updated_at
  BEFORE UPDATE ON atproto_authorizations
  FOR EACH ROW EXECUTE FUNCTION openchat_set_updated_at();
