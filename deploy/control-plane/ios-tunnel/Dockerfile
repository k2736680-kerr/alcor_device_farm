FROM alpine:3.22.1

ARG TUNNEL_UID=10001
ARG TUNNEL_GID=10001

RUN apk add --no-cache openssh-client \
    && addgroup -S -g "${TUNNEL_GID}" ios-tunnel \
    && adduser -S -D -H -u "${TUNNEL_UID}" -G ios-tunnel ios-tunnel

COPY entrypoint.sh /usr/local/bin/ios-tunnel-entrypoint
RUN chmod 0555 /usr/local/bin/ios-tunnel-entrypoint

USER ios-tunnel:ios-tunnel
ENTRYPOINT ["/usr/local/bin/ios-tunnel-entrypoint"]
