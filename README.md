# OpenChat Backend

Backend service for decentralized OpenChat server instances.

## Status
- Project maturity: `Pre-Alpha`
- Stability expectation: interfaces and behavior may change without backward compatibility until Beta.

## Current Focus
- E2EE-compatible channel message architecture.
- Membership-bound history access rules.
- Multi-device key envelope relay contracts.
- SFU-based WebRTC backend signaling and room orchestration.
- Go + Postgres module layout for implementation.

## Docs
- `docs/architecture/README.md`

## Local Run
```bash
go run ./cmd/openchatd
```

Default address: `:8080`

On startup, the server logs build metadata:
- `version`
- `commit`
- `build_time`
- `vcs_modified`

## RTC ICE Configuration
RTC ICE server URLs are configurable through environment variables and default to Google STUN.

Defaults:
- `OPENCHAT_RTC_STUN_URLS` defaults to:
  - `stun:stun.l.google.com:19302`
  - `stun:stun1.l.google.com:19302`
  - `stun:stun2.l.google.com:19302`
  - `stun:stun3.l.google.com:19302`
  - `stun:stun4.l.google.com:19302`
- `OPENCHAT_RTC_TURN_URLS` defaults to empty (no TURN advertised).

Config variables:
- `OPENCHAT_RTC_STUN_URLS` comma-separated STUN URLs.
- `OPENCHAT_RTC_TURN_URLS` comma-separated TURN/TURNS URLs.
- `OPENCHAT_RTC_TURN_USERNAME` TURN username.
- `OPENCHAT_RTC_TURN_CREDENTIAL` TURN credential/password.
- `OPENCHAT_RTC_TURN_CREDENTIAL_TYPE` TURN credential type (for example `static`).

To disable STUN advertisement entirely, set `OPENCHAT_RTC_STUN_URLS=none` (or `-`).

## RTC Subscribe Receive Limits
The backend advertises RTC subscribe receive caps and resolves effective limits per call using:
- `channel` override
- then `server` override
- then `instance` default

Defaults:
- `OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS=8`
- `OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS=16`

Optional override env vars (CSV `id=value`):
- `OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_SERVER`
- `OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_SERVER`
- `OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_CHANNEL`
- `OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_CHANNEL`

Examples:
- `OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_SERVER=srv_harbor=10,srv_testlab=6`
- `OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_CHANNEL=vc_general=20,tl_vc_huddle=12`

Wire contracts:
- `GET /v1/client/capabilities` includes `rtc.subscribe_receive_policy`.
- `POST /v1/rtc/channels/:channel_id/join-ticket` includes resolved `subscribe_receive_policy`.

## Docker Build (With Commit Metadata)
Docker builds now require a commit hash so runtime startup logs always reference the build commit.

```bash
docker build \
  --build-arg BUILD_VERSION=main \
  --build-arg BUILD_COMMIT=$(git rev-parse --verify HEAD) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t openchat-backend:dev .
```

For tagged builds, set `BUILD_VERSION` to the tag value you publish (for example `v1.2.3`).

## RTC Joiner (Legacy Audio Stream Tool)
Start a signaling client that joins a voice channel and streams audio over legacy `rtc.media.state` chunk payloads.
The production RTC v2 path uses WebRTC RTP audio/video tracks.

```bash
go run ./cmd/openchat-rtc-joiner \
  --channel-id vc_general \
  --file ./pina_colada.mp3 \
  --file-type mp3
```

Key flags:
- `--channel-id` (required): voice channel id.
- `--file`: file path to transmit.
- `--file-type`: label for transmitted file chunks (required with `--file`).
- `--media-mode`: `pcm-frames` (default) or `chunks`.
- `--ffmpeg-bin`: ffmpeg binary path used in `pcm-frames` mode.
- `--backend-url`: backend base URL (default `http://localhost:8080`).
- `--server-id`: server id for join ticket (default `srv_harbor`).
- `--loop`: replay file indefinitely.
- `--write-received-dir`: optional directory to reconstruct incoming streams from other joiners.

Example receiver that writes incoming streams:

```bash
go run ./cmd/openchat-rtc-joiner \
  --channel-id vc_general \
  --media-mode chunks \
  --write-received-dir ./tmp/incoming
```

## Implemented Endpoints (Current)
- `GET /healthz`
- `GET /v1/client/capabilities`
- `GET /v1/servers` (requester-scoped when identity headers are present)
- `DELETE /v1/servers/:server_id/membership`
- `GET /v1/profile/me`
- `PUT /v1/profile/me`
- `POST /v1/profile/avatar`
- `GET /v1/profile/avatar/{assetID}`
- `GET /v1/profiles:batch`
- `POST /v1/rtc/channels/:channel_id/join-ticket`
- `GET /v1/rtc/signaling` (WebSocket)

## Helm Chart
Chart path:
- `charts/openchat-backend`

Render locally:
```bash
helm template openchat-backend ./charts/openchat-backend
```

Install/upgrade:
```bash
helm upgrade --install openchat-backend ./charts/openchat-backend \
  --namespace openchat --create-namespace
```

Enable optional coturn (TURN + STUN on `3478`):
```yaml
coturn:
  enabled: true
  externalIP: "203.0.113.10"
  realm: "openchat.example.com"
  auth:
    username: "openchat-turn"
    password: "replace-me"
  backend:
    injectEnv: true
    advertisedTURNURLs:
      - "turn:turn.openchat.example.com:3478?transport=udp"
      - "turn:turn.openchat.example.com:3478?transport=tcp"
    advertisedSTUNURLs:
      - "stun:turn.openchat.example.com:3478"
    appendGoogleStunFallback: true
```

Notes:
- `coturn.externalIP`, `coturn.backend.advertisedTURNURLs`, and `coturn.backend.advertisedSTUNURLs` are required when coturn is enabled with backend env injection.
- When enabled, backend STUN advertisement is `advertisedSTUNURLs` first, then Google STUN fallback (unless disabled).
- Explicit `env.OPENCHAT_RTC_*` values still override coturn auto-injected RTC env.

OCI release flow:
- Push a git tag matching `chart-vX.X.X`.
- CI packages and publishes to GHCR as:
  - `ghcr.io/<owner>/charts/openchat-backend:X.X.X`
  - `ghcr.io/<owner>/charts/openchat-backend:chart-vX.X.X`

## License
This project is licensed under GNU General Public License v2.0 only (`GPL-2.0-only`).
See `LICENSE.md` for the full license text.
