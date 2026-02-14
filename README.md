# OpenChat Backend

Backend service for decentralized OpenChat server instances.

## Status
Early implementation stage.

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

## RTC Joiner (Audio Stream Test Tool)
Start a signaling client that joins a voice channel and streams audio over `rtc.media.state`.
Default mode (`pcm-frames`) decodes source audio to 48k mono PCM frames (via `ffmpeg`) for real-time-ish playback in the Electron client.

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

OCI release flow:
- Push a git tag matching `chart-vX.X.X`.
- CI packages and publishes to GHCR as:
  - `ghcr.io/<owner>/charts/openchat-backend:X.X.X`
  - `ghcr.io/<owner>/charts/openchat-backend:chart-vX.X.X`
