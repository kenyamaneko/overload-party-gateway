#!/usr/bin/env bash
set -euo pipefail

: "${PROJECT_ID:?PROJECT_ID env required}"
: "${REGION:?REGION env required}"
: "${MIG_NAME:?MIG_NAME env required}"
: "${NEW_TEMPLATE:?NEW_TEMPLATE env required}"

readonly ROLLOUT_TIMEOUT_SECONDS=900

gcloud compute instance-groups managed set-instance-template "${MIG_NAME}" \
  --template="${NEW_TEMPLATE}" \
  --region="${REGION}" \
  --project="${PROJECT_ID}"

# max-surge / max-unavailable 等の更新ポリシーは Terraform が MIG 自体の設定として
# 所有するため、ここでは対象の instance template を指定するのみで上書きしない。
gcloud compute instance-groups managed rolling-action start-update "${MIG_NAME}" \
  --version="template=${NEW_TEMPLATE}" \
  --region="${REGION}" \
  --project="${PROJECT_ID}"

gcloud compute instance-groups managed wait-until "${MIG_NAME}" \
  --stable \
  --region="${REGION}" \
  --project="${PROJECT_ID}" \
  --timeout="${ROLLOUT_TIMEOUT_SECONDS}"
