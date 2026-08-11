#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
upstream_repository="${DEVICE_FARM_ANDROID_UPSTREAM_REPOSITORY:-https://github.com/budtmo/docker-android.git}"
upstream_commit="${DEVICE_FARM_ANDROID_UPSTREAM_COMMIT:-e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2}"
source_cache="${DEVICE_FARM_ANDROID_SOURCE_CACHE:-}"
base_image="${DEVICE_FARM_ANDROID_BASE_IMAGE:-alcor-df-build/docker-android-base:v3.5.2-p0}"
output_repository="${DEVICE_FARM_ANDROID_REPOSITORY:-alcor-device-farm/android-emulator}"
temporary_dir="$(mktemp -d)"
: "${DEVICE_FARM_SDKMANAGER_VERSION:?set the pinned Android command-line tools version}"

cleanup() {
  rm -rf -- "$temporary_dir"
}
trap cleanup EXIT

command -v docker >/dev/null
command -v git >/dev/null
if [[ ! "${DEVICE_FARM_ANDROID_PREPARE_REQUEST:-}" =~ ^api(33|34|35|36)-(google_apis|google_play)-(x86_64)-sdk([A-Za-z0-9._-]+)$ ]]; then
  echo "build.sh is internal to the controlled Build Agent and requires a validated preparation selector" >&2
  exit 64
fi
api_level="${BASH_REMATCH[1]}"
image_type="${BASH_REMATCH[2]}"
abi="${BASH_REMATCH[3]}"
revision="${BASH_REMATCH[4]}"
android_version="$((api_level - 20)).0"
tag="${android_version}-api${api_level}-${image_type}-${abi}-sdk${revision}"
case "$base_image" in
  *:latest|latest)
    echo "floating latest base images are not allowed" >&2
    exit 1
    ;;
esac

source_dir="$temporary_dir/docker-android"
if [ -n "$source_cache" ]; then
  cached_commit="$(git -C "$source_cache" rev-parse HEAD)"
  if [ "$cached_commit" != "$upstream_commit" ]; then
    echo "source cache commit $cached_commit does not match $upstream_commit" >&2
    exit 1
  fi
  git clone -q --no-hardlinks "$source_cache" "$source_dir"
  git -C "$source_dir" checkout -q --detach "$upstream_commit"
else
  git init -q "$source_dir"
  git -C "$source_dir" remote add origin "$upstream_repository"
  git -C "$source_dir" fetch -q --depth 1 origin "$upstream_commit"
  git -C "$source_dir" checkout -q --detach FETCH_HEAD
fi
git -C "$source_dir" apply --check "$script_dir/patches/upstream.patch"
git -C "$source_dir" apply "$script_dir/patches/upstream.patch"
git -C "$source_dir" apply --check "$script_dir/../android16/uiautomator2-preinstall.patch"
git -C "$source_dir" apply "$script_dir/../android16/uiautomator2-preinstall.patch"

if [ "${DEVICE_FARM_ANDROID_SKIP_BASE_BUILD:-0}" = "1" ]; then
  docker image inspect "$base_image" >/dev/null
  echo "USING_VERIFIED_BASE image=$base_image"
else
  docker build \
    --build-arg DOCKER_ANDROID_VERSION=v3.5.2-p0 \
    --tag "$base_image" \
    --file "$source_dir/docker/base" \
    "$source_dir"
fi

output_image="$output_repository:$tag"
echo "BUILDING api=$api_level type=$image_type abi=$abi revision=$revision image=$output_image"
docker build \
  --build-arg BASE_IMAGE="$base_image" \
  --build-arg ANDROID_VERSION="$android_version" \
  --build-arg API_LEVEL="$api_level" \
  --build-arg SYSTEM_IMAGE_ABI="$abi" \
  --build-arg SYSTEM_IMAGE_TYPE="$image_type" \
  --build-arg SYSTEM_IMAGE_REVISION="$revision" \
  --build-arg SDKMANAGER_VERSION="$DEVICE_FARM_SDKMANAGER_VERSION" \
  --build-arg UPSTREAM_COMMIT="$upstream_commit" \
  --tag "$output_image" \
  --file "$script_dir/Dockerfile" \
  "$source_dir"
docker image inspect "$output_image" --format 'IMAGE={{index .RepoTags 0}} ID={{.Id}} SIZE={{.Size}}'
docker run --rm --entrypoint appium "$output_image" --version
docker run --rm --entrypoint appium "$output_image" driver list --installed
