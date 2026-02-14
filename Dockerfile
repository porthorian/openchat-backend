# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG BUILD_VERSION=main
ARG BUILD_COMMIT=unknown
ARG BUILD_TIME=unknown
RUN test -n "${BUILD_COMMIT}" && \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
  go build -trimpath \
  -ldflags="-s -w -X github.com/openchat/openchat-backend/internal/app.BuildVersion=${BUILD_VERSION} -X github.com/openchat/openchat-backend/internal/app.BuildCommit=${BUILD_COMMIT} -X github.com/openchat/openchat-backend/internal/app.BuildTime=${BUILD_TIME}" \
  -o /out/openchatd ./cmd/openchatd

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata \
  && addgroup -S openchat \
  && adduser -S -G openchat openchat

WORKDIR /home/openchat
COPY --from=builder /out/openchatd /usr/local/bin/openchatd

USER openchat

EXPOSE 8080

ENV OPENCHAT_HTTP_ADDR=:8080 \
  OPENCHAT_PUBLIC_BASE_URL=http://localhost:8080 \
  OPENCHAT_SIGNALING_PATH=/v1/rtc/signaling \
  OPENCHAT_JOIN_TICKET_TTL_SECONDS=60 \
  OPENCHAT_JOIN_TICKET_SECRET=dev-insecure-secret-change-me \
  OPENCHAT_ENV=development

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/openchatd"]
