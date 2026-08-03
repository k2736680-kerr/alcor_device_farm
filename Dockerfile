ARG GO_IMAGE=golang:1.24.6-alpine3.22
ARG RUNTIME_IMAGE=alpine:3.22.1

FROM ${GO_IMAGE} AS builder
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.version=${VERSION} -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.commit=${COMMIT} -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.buildDate=${BUILD_DATE}" \
    -o /out/device-farm-server ./cmd/device-farm-server

FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 65532 devicefarm && adduser -S -D -H -u 65532 -G devicefarm devicefarm
COPY --from=builder /out/device-farm-server /usr/local/bin/device-farm-server
COPY scripts/check-server-deployment.sh /usr/local/bin/check-server-deployment.sh
COPY scripts/device-farm-server-entrypoint.sh /usr/local/bin/device-farm-server-entrypoint.sh
RUN chmod 0755 /usr/local/bin/device-farm-server /usr/local/bin/check-server-deployment.sh /usr/local/bin/device-farm-server-entrypoint.sh
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/device-farm-server-entrypoint.sh"]
