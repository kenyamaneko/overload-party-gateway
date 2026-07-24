#!/usr/bin/env bash
# create-versioned-template.sh — 元の instance template の設定を引き継ぎ、
# コンテナイメージだけ差し替えた新規 instance template を作成する。
#
# gcloud に instance template を複製するフラグが無いため、describe した設定から
# create-with-container 向けに主要フィールドを再構成する。元の instance template
# が持つ設定のうち本スクリプトが未対応のフィールドは引き継がれない。
set -euo pipefail

: "${REGISTRY_PROJECT_ID:?REGISTRY_PROJECT_ID env required}"
: "${PROJECT_ID:?PROJECT_ID env required}"
: "${BASE_TEMPLATE:?BASE_TEMPLATE env required}"
: "${MIG_NAME:?MIG_NAME env required}"
: "${IMAGE:?IMAGE env required}"
: "${SHA:?SHA env required}"
: "${RUN_NUMBER:?RUN_NUMBER env required}"

readonly TEMPLATE_NAME_SHA_LENGTH=12

CONTAINER_IMAGE="${IMAGE}:${SHA}"

if ! DESCRIBE_ERROR=$(gcloud artifacts docker images describe "${CONTAINER_IMAGE}" \
  --project="${REGISTRY_PROJECT_ID}" 2>&1 >/dev/null); then
  echo "::error::Image not found or inaccessible: ${CONTAINER_IMAGE}"
  echo "${DESCRIBE_ERROR}" >&2
  exit 1
fi

BASE_JSON=$(gcloud compute instance-templates describe "${BASE_TEMPLATE}" \
  --project="${PROJECT_ID}" --format=json)

extract() {
  printf '%s' "${BASE_JSON}" | jq -r "$1"
}

get_short_name() {
  printf '%s' "$1" | sed 's#.*/##'
}

require_field() {
  local field_name="$1"
  local value="$2"
  if [ -z "${value}" ] || [ "${value}" = "null" ]; then
    echo "::error::Base template ${BASE_TEMPLATE} is missing required field: ${field_name}"
    exit 1
  fi
}

MACHINE_TYPE=$(get_short_name "$(extract '.properties.machineType')")
SERVICE_ACCOUNT=$(extract '.properties.serviceAccounts[0].email')
SCOPES=$(extract '.properties.serviceAccounts[0].scopes | join(",")')
NETWORK=$(extract '.properties.networkInterfaces[0].network')
SUBNET=$(extract '.properties.networkInterfaces[0].subnetwork // empty')
TAGS=$(extract '.properties.tags.items // [] | join(",")')
LABELS=$(extract '.properties.labels // {} | to_entries | map("\(.key)=\(.value)") | join(",")')
BOOT_IMAGE=$(extract '.properties.disks[] | select(.boot) | .initializeParams.sourceImage')
BOOT_DISK_SIZE=$(extract '.properties.disks[] | select(.boot) | .initializeParams.diskSizeGb')
BOOT_DISK_TYPE=$(get_short_name "$(extract '.properties.disks[] | select(.boot) | .initializeParams.diskType')")

require_field "machineType" "${MACHINE_TYPE}"
require_field "serviceAccounts[0].email" "${SERVICE_ACCOUNT}"
require_field "networkInterfaces[0].network" "${NETWORK}"
require_field "disks[boot].initializeParams.sourceImage" "${BOOT_IMAGE}"

METADATA_DIR=$(mktemp -d)
METADATA_FLAG=""
METADATA_KEYS=$(extract '.properties.metadata.items[]? | select(.key != "gce-container-declaration") | .key')
while IFS= read -r key; do
  [ -z "${key}" ] && continue
  file="${METADATA_DIR}/${key}"
  # @tsv は値中の改行・タブをエスケープするため、キー単位で個別に取り出し
  # metadata の値 (複数行の起動スクリプト等) を改変せず引き継ぐ。
  printf '%s' "${BASE_JSON}" | jq -r --arg k "${key}" \
    '.properties.metadata.items[] | select(.key == $k) | .value' > "${file}"
  if [ -z "${METADATA_FLAG}" ]; then
    METADATA_FLAG="${key}=${file}"
  else
    METADATA_FLAG="${METADATA_FLAG},${key}=${file}"
  fi
done <<< "${METADATA_KEYS}"

SHORT_SHA="${SHA:0:${TEMPLATE_NAME_SHA_LENGTH}}"
NEW_TEMPLATE="${MIG_NAME}-${SHORT_SHA}-${RUN_NUMBER}"

ARGS=(
  "${NEW_TEMPLATE}"
  "--project=${PROJECT_ID}"
  "--machine-type=${MACHINE_TYPE}"
  "--service-account=${SERVICE_ACCOUNT}"
  "--network=${NETWORK}"
  "--image=${BOOT_IMAGE}"
  "--boot-disk-size=${BOOT_DISK_SIZE}GB"
  "--container-image=${CONTAINER_IMAGE}"
)

[ -n "${SCOPES}" ] && ARGS+=("--scopes=${SCOPES}")
[ -n "${SUBNET}" ] && ARGS+=("--subnet=${SUBNET}")
[ -n "${TAGS}" ] && ARGS+=("--tags=${TAGS}")
[ -n "${LABELS}" ] && ARGS+=("--labels=${LABELS}")
[ -n "${BOOT_DISK_TYPE}" ] && [ "${BOOT_DISK_TYPE}" != "null" ] && ARGS+=("--boot-disk-type=${BOOT_DISK_TYPE}")
[ -n "${METADATA_FLAG}" ] && ARGS+=("--metadata-from-file=${METADATA_FLAG}")

gcloud compute instance-templates create-with-container "${ARGS[@]}"

echo "template=${NEW_TEMPLATE}" >> "${GITHUB_OUTPUT}"
