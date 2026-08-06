#!/usr/bin/env sh
set -eu

: "${DEVICE_FARM_CONSOLE_ORIGIN:?set DEVICE_FARM_CONSOLE_ORIGIN, for example https://farm.example.internal}"
origin=${DEVICE_FARM_CONSOLE_ORIGIN%/}
curl_tls_args=
if [ "${DEVICE_FARM_CONSOLE_INSECURE_TLS:-}" = "1" ]; then
  curl_tls_args=--insecure
fi

case "$origin" in
  https://*) require_hsts=1 ;;
  http://127.0.0.1:*|http://localhost:*)
    if [ "${DEVICE_FARM_CONSOLE_ALLOW_HTTP:-}" != "1" ]; then
      echo "loopback HTTP verification requires DEVICE_FARM_CONSOLE_ALLOW_HTTP=1" >&2
      exit 1
    fi
    require_hsts=0
    ;;
  *)
    echo "Console verification requires HTTPS; plain HTTP is allowed only on loopback" >&2
    exit 1
    ;;
esac

temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

curl --fail --silent --show-error --max-time 10 \
  $curl_tls_args \
  --dump-header "$temporary_dir/entry.headers" \
  --output "$temporary_dir/index.html" \
  "$origin/console/"

header_value() {
  name=$1
  sed -n "s/^${name}:[[:space:]]*//Ip" "$temporary_dir/entry.headers" | tr -d '\r' | tail -n 1
}

[ "$(header_value cache-control)" = "no-store" ] || {
  echo "Console entry must return Cache-Control: no-store" >&2
  exit 1
}
[ "$(header_value x-frame-options)" = "DENY" ] || {
  echo "Console entry must return X-Frame-Options: DENY" >&2
  exit 1
}
header_value content-security-policy | grep -F "default-src 'self'" >/dev/null || {
  echo "Console entry is missing the required Content-Security-Policy" >&2
  exit 1
}
if [ "$require_hsts" -eq 1 ]; then
  header_value strict-transport-security | grep -F "max-age=" >/dev/null || {
    echo "HTTPS Console entry is missing Strict-Transport-Security" >&2
    exit 1
  }
fi

asset=$(sed -n 's/.*src="\/console\/\(assets\/index-[^"]*\.js\)".*/\1/p' "$temporary_dir/index.html" | head -n 1)
[ -n "$asset" ] || {
  echo "Console entry did not reference a content-hashed JavaScript asset" >&2
  exit 1
}
curl --fail --silent --show-error --max-time 10 \
  $curl_tls_args \
  --dump-header "$temporary_dir/asset.headers" \
  --output "$temporary_dir/app.js" \
  "$origin/console/$asset"
grep -i '^cache-control:[[:space:]]*public, max-age=31536000, immutable' "$temporary_dir/asset.headers" >/dev/null || {
  echo "Console hashed asset is missing the immutable cache policy" >&2
  exit 1
}

unauthenticated_status=$(curl --silent --show-error --max-time 10 \
  $curl_tls_args \
  --output "$temporary_dir/unauthenticated.json" --write-out '%{http_code}' \
  "$origin/api/v1/device-images")
[ "$unauthenticated_status" = "401" ] || {
  echo "Unauthenticated device API returned HTTP $unauthenticated_status instead of 401" >&2
  exit 1
}
grep -F '"code":"UNAUTHORIZED"' "$temporary_dir/unauthenticated.json" >/dev/null || {
  echo "Unauthenticated device API did not return the stable UNAUTHORIZED code" >&2
  exit 1
}

curl --fail --silent --show-error --max-time 10 $curl_tls_args "$origin/healthz" >/dev/null
curl --fail --silent --show-error --max-time 10 $curl_tls_args "$origin/readyz" >/dev/null

if grep -E -i 'DEVICE_FARM_(SECURITY_(SERVICE|AGENT)_TOKEN|STF_API_TOKEN)|postgres(ql)?://|Bearer[[:space:]]+[A-Za-z0-9._~-]{20,}' \
  "$temporary_dir/index.html" "$temporary_dir/app.js" >/dev/null; then
  echo "Console production assets contain a forbidden internal credential marker" >&2
  exit 1
fi

echo "Console deployment verification passed"
