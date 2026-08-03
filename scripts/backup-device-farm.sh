#!/usr/bin/env sh
set -eu

if [ -z "${DEVICE_FARM_DATABASE_URL:-}" ] || [ "$#" -ne 1 ]; then
  echo "usage: DEVICE_FARM_DATABASE_URL=... $0 <backup-directory>" >&2
  exit 2
fi
backup_directory=$1
pg_dump_binary="${DEVICE_FARM_PG_DUMP:-pg_dump}"
"$pg_dump_binary" --version >/dev/null
sha256sum --version >/dev/null
mkdir -p "$backup_directory"
backup_directory=$(CDPATH= cd -- "$backup_directory" && pwd)
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
temporary_file=$(mktemp "$backup_directory/.device-farm-$timestamp.XXXXXX")
final_file="$backup_directory/device-farm-$timestamp.dump"
trap 'rm -f "$temporary_file"' EXIT HUP INT TERM

"$pg_dump_binary" "$DEVICE_FARM_DATABASE_URL" --format=custom --no-owner --no-privileges --file="$temporary_file"
chmod 0600 "$temporary_file"
mv "$temporary_file" "$final_file"
trap - EXIT HUP INT TERM
sha256sum "$final_file" > "$final_file.sha256"
chmod 0600 "$final_file.sha256"
echo "$final_file"
