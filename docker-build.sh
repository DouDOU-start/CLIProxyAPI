#!/usr/bin/env bash
#
# build.sh - Linux Build Script
#
# This script builds the current source and starts the application and PostgreSQL containers.

set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "Error: unknown option '${1}'."
  echo "Usage: ./docker-build.sh"
  exit 1
fi

VERSION="$(git describe --tags --always --dirty)"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "Building with the following info:"
echo "  Version: ${VERSION}"
echo "  Commit: ${COMMIT}"
echo "  Build Date: ${BUILD_DATE}"
echo "----------------------------------------"

export CLI_PROXY_IMAGE="cli-proxy-api:local"

echo "Building the application image..."
docker compose build \
  --build-arg VERSION="${VERSION}" \
  --build-arg COMMIT="${COMMIT}" \
  --build-arg BUILD_DATE="${BUILD_DATE}" \
  cli-proxy-api

echo "Starting the application and PostgreSQL containers..."
docker compose up -d --remove-orphans --pull never

echo "Build complete. Services are running."
if [[ -n "${COMPOSE_PROJECT_NAME:-}" ]]; then
  echo "Run 'docker compose -p ${COMPOSE_PROJECT_NAME} logs -f' to see the logs."
else
  echo "Run 'docker compose logs -f' to see the logs."
fi
