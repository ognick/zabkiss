FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG APP_VERSION=dev

WORKDIR /build

# Кэшируем зависимости отдельным слоем — пересчитывается только при изменении go.mod/go.sum
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY backend/ .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X main.version=${APP_VERSION}" -o /zabkiss ./cmd/zabkiss

FROM alpine:3.21
RUN apk add --no-cache jq

COPY --from=builder /zabkiss /usr/bin/zabkiss
COPY run.sh /run.sh
RUN chmod a+x /run.sh

ENTRYPOINT ["/run.sh"]
