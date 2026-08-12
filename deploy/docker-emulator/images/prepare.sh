#!/usr/bin/env bash
# This is the only Build Agent entrypoint for a requested Android System Image.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
package_name="${1:?usage: prepare.sh 'system-images;android-<api>;<type>;<abi>' <revision>}"
revision="${2:?official package revision is required}"
IFS=';' read -r package_kind android_package image_type abi extra <<< "$package_name"
api_level="${android_package#android-}"

# The Console never supplies a URL or a shell fragment. These are the only
# combinations currently exposed by the stable-channel synchronizer.
if [[ ! "$package_kind:$api_level:$image_type:$abi:${extra:-}" =~ ^system-images:(2[6-9]|[3-9][0-9]):(default|google_apis|google_play):x86_64:$ ]]; then
  echo "unsupported stable Android System Image selection" >&2
  exit 64
fi
case "$revision" in *[!A-Za-z0-9._-]*|'') echo "invalid Android SDK package revision" >&2; exit 64;; esac

case "${DEVICE_FARM_ANDROID_REPOSITORY:-alcor-device-farm/android-emulator}" in
  *:latest|latest) echo "floating image tags are not allowed" >&2; exit 64 ;;
esac

case "$api_level" in
  26) android_version=8.0 ;; 27) android_version=8.1 ;; 28) android_version=9.0 ;; 29) android_version=10.0 ;;
  30) android_version=11.0 ;; 31) android_version=12.0 ;; 32) android_version=12.1 ;; 33) android_version=13.0 ;;
  34) android_version=14.0 ;; 35) android_version=15.0 ;; 36) android_version=16.0 ;;
  *) android_version="api${api_level}" ;;
esac
tag="${android_version}-api${api_level}-${image_type}-${abi}-sdk${revision}"
local_repository="${DEVICE_FARM_ANDROID_REPOSITORY:-alcor-device-farm/android-emulator}"
: "${DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY:?set the controlled Registry repository}"
case "$DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY" in http://*|https://*|*:latest|latest) echo "invalid publish repository" >&2; exit 64;; esac

# build.sh accepts only catalog selectors and pins the upstream commit. Its
# sdkmanager invocation names the selected official package explicitly.
export DEVICE_FARM_ANDROID_PREPARE_REQUEST="api${api_level}-${image_type}-${abi}-sdk${revision}"
"$script_dir/build.sh"
source_image="$local_repository:$tag"
target_image="$DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY:$tag"
docker tag "$source_image" "$target_image"
docker push "$target_image"
digest_ref="$(docker image inspect "$target_image" --format '{{range .RepoDigests}}{{println .}}{{end}}' | awk -v repository="$DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY" 'index($0, repository "@sha256:")==1 {print; exit}')"
if [[ ! "$digest_ref" =~ @sha256:[a-f0-9]{64}$ ]]; then echo "cannot resolve pushed digest" >&2; exit 1; fi
size_bytes="$(docker image inspect "$target_image" --format '{{.Size}}')"
digest="${digest_ref##*@}"
image_disk_mb="$(( (size_bytes + 1024 * 1024 - 1) / (1024 * 1024) ))"
printf 'DEVICE_FARM_IMAGE_RESULT={"docker_image":"%s","docker_digest":"%s","image_disk_mb":%s}\n' "$target_image" "$digest" "$image_disk_mb"
