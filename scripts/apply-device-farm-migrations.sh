#!/usr/bin/env sh
set -eu

usage() {
  echo "usage: $0 --fresh | <migration.up.sql> [...]" >&2
  exit 2
}

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
migration_root="${DEVICE_FARM_MIGRATION_DIR:-$repository_root/migrations}"
psql_binary="${DEVICE_FARM_PSQL:-psql}"

if [ -z "${DEVICE_FARM_DATABASE_URL:-}" ] || [ "$#" -eq 0 ]; then
  usage
fi
"$psql_binary" --version >/dev/null

if [ "$1" = "--fresh" ]; then
  if [ "$#" -ne 1 ]; then
    usage
  fi
  existing=$($psql_binary "$DEVICE_FARM_DATABASE_URL" -X -A -t -v ON_ERROR_STOP=1 \
    -c "SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname IN ('devices','device_reservations','device_hosts')")
  if [ "$existing" != "0" ]; then
    echo "--fresh refuses to run because Device Farm tables already exist" >&2
    exit 1
  fi
  for migration_file in "$migration_root"/*.up.sql; do
    echo "applying $(basename "$migration_file")"
    "$psql_binary" "$DEVICE_FARM_DATABASE_URL" -X -v ON_ERROR_STOP=1 --single-transaction -f "$migration_file"
  done
  exit 0
else
  for requested in "$@"; do
    case "$requested" in
      */*|*\\*|.*|*.down.sql|*.sql.sql) echo "migration must be a basename ending in .up.sql" >&2; exit 1 ;;
      *.up.sql) ;;
      *) echo "migration must end in .up.sql" >&2; exit 1 ;;
    esac
    migration_file="$migration_root/$requested"
    if [ ! -f "$migration_file" ]; then
      echo "migration does not exist: $requested" >&2
      exit 1
    fi
    echo "applying $requested"
    "$psql_binary" "$DEVICE_FARM_DATABASE_URL" -X -v ON_ERROR_STOP=1 --single-transaction -f "$migration_file"
  done
fi
