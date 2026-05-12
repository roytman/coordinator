#!/bin/bash

# Clone llm-d-inference-scheduler at pinned commit and build EPP image.
set -eo pipefail

REPO_URL="https://github.com/llm-d/llm-d-inference-scheduler.git"
COMMIT="434575493d5bf3a5c7a5b9b1d6210ca6228ce076"

: "${EPP_IMAGE:?not set — run via 'make image-build-epp' or export EPP_IMAGE}"
: "${CONTAINER_RUNTIME:?not set — run via 'make image-build-epp'}"

if "${CONTAINER_RUNTIME}" image inspect "${EPP_IMAGE}" >/dev/null 2>&1; then
    echo "${EPP_IMAGE} already present locally; skipping build (remove image to force rebuild)"
    exit 0
fi

CLONE_DIR="$(mktemp -d)"
trap 'rm -rf "${CLONE_DIR}"' EXIT

git -C "${CLONE_DIR}" init -q
git -C "${CLONE_DIR}" remote add origin "${REPO_URL}"
git -C "${CLONE_DIR}" fetch --depth=1 --no-tags origin "${COMMIT}"
git -C "${CLONE_DIR}" -c advice.detachedHead=false checkout FETCH_HEAD

"${CONTAINER_RUNTIME}" build \
    --build-arg LDFLAGS="-s -w" \
    -t "${EPP_IMAGE}" \
    -f "${CLONE_DIR}/Dockerfile.epp" \
    "${CLONE_DIR}"

echo "Built ${EPP_IMAGE}"
