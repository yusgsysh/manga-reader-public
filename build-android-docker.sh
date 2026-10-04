#!/bin/bash
# Build Android APK using Podman/Docker
# Builds the Android APK entirely inside the container, then copies the output.
#
# Usage:
#   ./build-android-docker.sh            # debug APK
#   TARGET=android:package ./build-android-docker.sh   # signed release APK
#
# Note: `podman run` MUST NOT use --rm, otherwise the container (and the APK
# inside it) is gone before `podman cp` runs.

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="wails-android"
CONTAINER_NAME="wails-android-build"
ENGINE="${CONTAINER_ENGINE:-podman}"
TARGET="${TARGET:-android:build android:assemble:apk}"

APK_IN_CONTAINER="/app/backend/cmd/desktop/bin/manga-reader-desktop.apk"

echo "=== Building Android container image ==="
"$ENGINE" build -t "$IMAGE_NAME" \
  -f "$PROJECT_ROOT/backend/cmd/desktop/build/docker/Dockerfile.android" \
  "$PROJECT_ROOT"

echo "=== Removing any stale build container ==="
"$ENGINE" rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true

echo "=== Running Android build inside container ==="
"$ENGINE" run --name "$CONTAINER_NAME" \
  -w /app/backend/cmd/desktop \
  "$IMAGE_NAME" $TARGET

echo "=== Copying APK out of the container ==="
"$ENGINE" cp "$CONTAINER_NAME:$APK_IN_CONTAINER" "$PROJECT_ROOT/manga-reader-desktop.apk"

echo "=== Cleaning up container ==="
"$ENGINE" rm -f "$CONTAINER_NAME" >/dev/null

echo "=== Build complete ==="
echo "APK: $PROJECT_ROOT/manga-reader-desktop.apk"
