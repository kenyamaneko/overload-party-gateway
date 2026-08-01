#!/usr/bin/env bash
set -euo pipefail

FIRESTORE_PORT=9041
GOOGLE_CLOUD_PROJECT_ID=overload-party-test
HEALTH_CHECK_TIMEOUT_SEC=30

gcloud emulators firestore start --host-port="localhost:${FIRESTORE_PORT}" >/tmp/firestore.log 2>&1 &
started=false
for _ in $(seq 1 "${HEALTH_CHECK_TIMEOUT_SEC}"); do
  if curl -sf "http://localhost:${FIRESTORE_PORT}" >/dev/null; then
    started=true
    break
  fi
  sleep 1
done
if [ "$started" = false ]; then
  echo "Firestore emulator failed to start"
  cat /tmp/firestore.log
  exit 1
fi

{
  echo "FIRESTORE_EMULATOR_HOST=localhost:${FIRESTORE_PORT}"
  echo "GOOGLE_CLOUD_PROJECT_ID=${GOOGLE_CLOUD_PROJECT_ID}"
} >>"$GITHUB_ENV"
