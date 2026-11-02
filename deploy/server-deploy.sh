#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
umask 077

# A forced SSH command: never evaluate text sent by the SSH client.
if [[ -n ${SSH_ORIGINAL_COMMAND:-} ]]; then
  read -r operation image extra <<< "$SSH_ORIGINAL_COMMAND"
else
  operation=${1:-}
  image=${2:-}
  extra=${3:-}
fi
if [[ -n "$extra" ]]; then
  echo 'Unexpected arguments.' >&2
  exit 2
fi
case "$operation" in
  deploy)
    if [[ ! "$image" =~ ^ghcr\.io/bed787/dorm-hub@sha256:[a-f0-9]{64}$ ]]; then
      echo 'Only a digest from the dorm-hub image registry is accepted.' >&2
      exit 2
    fi
    ;;
  rollback)
    [[ -z "$image" ]] || exit 2
    ;;
  *) echo 'Allowed commands: deploy IMAGE_DIGEST, rollback.' >&2; exit 2 ;;
esac

exec 9>/run/lock/dorm-hub-deploy.lock
flock -w 180 9
cd /opt/dorm-hub
state=/var/lib/dorm-hub-deploy
mkdir -p "$state"
compose=(docker compose --project-name dorm-hub --project-directory /opt/dorm-hub
  --env-file /opt/dorm-hub/.env.production -f /etc/dorm-hub/compose.registry.yaml)
if [[ -f /etc/dorm-hub/compose.monitoring.yaml ]]; then
  compose+=(-f /etc/dorm-hub/compose.monitoring.yaml)
fi

start_image() {
  APP_IMAGE="$1" "${compose[@]}" up -d --no-deps --no-build --pull never \
    --wait --wait-timeout 120 app
}

container=$(docker ps -aq --filter 'label=com.docker.compose.project=dorm-hub' \
  --filter 'label=com.docker.compose.service=app')
previous=''
if [[ -n "$container" ]]; then
  # Use the local immutable image ID, including for the first deployment.
  previous=$(docker inspect --format '{{.Image}}' "$container")
fi

if [[ "$operation" == rollback ]]; then
  [[ -s "$state/previous-image" ]] || { echo 'No previous image saved.' >&2; exit 1; }
  image=$(cat "$state/previous-image")
  docker image inspect "$image" >/dev/null
  start_image "$image"
  printf '%s\n' "$image" > "$state/current-image"
  [[ -z "$previous" ]] || printf '%s\n' "$previous" > "$state/previous-image"
  echo 'Previous application image restored. Database was not rolled back.'
  exit 0
fi


IFS= read -r -t 30 registry_token
[[ -n "$registry_token" ]] || { echo 'Missing registry token.' >&2; exit 1; }
auth_dir=$(mktemp -d /run/dorm-hub-registry.XXXXXX)
trap 'rm -rf "$auth_dir"' EXIT
export DOCKER_CONFIG="$auth_dir"
printf '%s' "$registry_token" | docker login ghcr.io -u bed787 --password-stdin
unset registry_token

export APP_IMAGE="$image"
"${compose[@]}" config --quiet
"${compose[@]}" pull app

if start_image "$image"; then
  [[ -z "$previous" ]] || printf '%s\n' "$previous" > "$state/previous-image"
  printf '%s\n' "$image" > "$state/current-image"
  echo "Deployment healthy: $image"
else
  echo 'New application failed its health check.' >&2
  if [[ -n "$previous" ]]; then
    echo 'Trying to restore the previous application image.' >&2
    if start_image "$previous"; then
      echo 'Previous application restored. Database was not rolled back.' >&2
    else
      echo 'Restoration failed; inspect application logs.' >&2
    fi
  fi
  exit 1
fi
