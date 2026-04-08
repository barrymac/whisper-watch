# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ARG TARGETARCH
WORKDIR /app

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -mod=vendor -ldflags="-s -w" -o /whisper-bot ./cmd/whisper-bot

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /whisper-bot /usr/local/bin/whisper-bot

EXPOSE 8080

ENTRYPOINT ["whisper-bot"]
