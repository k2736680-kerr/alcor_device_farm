#!/usr/bin/env sh
set -eu

/usr/local/bin/check-server-deployment.sh
exec /usr/local/bin/device-farm-server "$@"
