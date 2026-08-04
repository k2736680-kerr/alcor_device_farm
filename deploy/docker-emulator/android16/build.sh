#!/usr/bin/env bash
set -euo pipefail

upstream_repository="${DEVICE_FARM_ANDROID_UPSTREAM_REPOSITORY:-https://github.com/budtmo/docker-android.git}"
upstream_commit="${DEVICE_FARM_ANDROID_UPSTREAM_COMMIT:-e5e31745bfca26d7e71eaf3cbd84767ce5d57fd2}"
base_image="${DEVICE_FARM_ANDROID_BASE_IMAGE:-alcor-df-build/docker-android-base:v3.5.2-p0}"
output_image="${DEVICE_FARM_ANDROID_OUTPUT_IMAGE:-alcor-device-farm/android-emulator:16.0-api36-r3}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
temporary_dir="$(mktemp -d)"

cleanup() {
  rm -rf -- "$temporary_dir"
}
trap cleanup EXIT

command -v docker >/dev/null
command -v git >/dev/null

source_dir="$temporary_dir/docker-android"
git init -q "$source_dir"
git -C "$source_dir" remote add origin "$upstream_repository"
git -C "$source_dir" fetch -q --depth 1 origin "$upstream_commit"
git -C "$source_dir" checkout -q --detach FETCH_HEAD
git -C "$source_dir" apply --check "$script_dir/upstream.patch"
git -C "$source_dir" apply "$script_dir/upstream.patch"
git -C "$source_dir" apply --check "$script_dir/uiautomator2-preinstall.patch"
git -C "$source_dir" apply "$script_dir/uiautomator2-preinstall.patch"

docker build \
  --build-arg DOCKER_ANDROID_VERSION=v3.5.2-p0 \
  --tag "$base_image" \
  --file "$source_dir/docker/base" \
  "$source_dir"

docker build \
  --build-arg BASE_IMAGE="$base_image" \
  --tag "$output_image" \
  --file "$script_dir/Dockerfile" \
  "$source_dir"

docker image inspect "$output_image" --format 'IMAGE_ID={{.Id}}'
docker run --rm --entrypoint appium "$output_image" --version
docker run --rm --entrypoint appium "$output_image" driver list --installed
