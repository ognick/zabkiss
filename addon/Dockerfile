FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY backend/ .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /zabkiss ./cmd/zabkiss

FROM alpine:3.21
RUN apk add --no-cache jq

COPY --from=builder /zabkiss /usr/bin/zabkiss
COPY run.sh /run.sh
RUN chmod a+x /run.sh

ENTRYPOINT ["/run.sh"]
