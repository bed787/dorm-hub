#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Run this installer as root on the VPS.' >&2; exit 1; }
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
test -f /opt/dorm-hub/.env.production
command -v docker >/dev/null
command -v flock >/dev/null
docker compose version
[[ $# == 0 || ( $# == 1 && $1 == --search ) ]] || { echo 'Usage: install-server.sh [--search]' >&2; exit 2; }
command -v openssl >/dev/null
exec 9>/run/lock/dorm-hub-deploy.lock
flock -w 180 9
if ! grep -q '^MEILI_MASTER_KEY=' /opt/dorm-hub/.env.production; then
  printf '\nMEILI_MASTER_KEY=%s\n' "$(openssl rand -hex 32)" >> /opt/dorm-hub/.env.production
fi
chmod 600 /opt/dorm-hub/.env.production
mkdir -p /etc/dorm-hub /var/lib/dorm-hub-deploy /root/.ssh
chmod 700 /root/.ssh

if [[ ${1:-} == --search ]]; then
  state=/var/lib/dorm-hub-deploy
  test -s "$state/current-image"
  image=$(cat "$state/current-image")
  docker image inspect "$image" >/dev/null
  compose=(docker compose --project-name dorm-hub --project-directory /opt/dorm-hub
    --env-file /opt/dorm-hub/.env.production -f /etc/dorm-hub/compose.registry.yaml)
  if [[ -f /etc/dorm-hub/compose.monitoring.yaml ]]; then
    compose+=(-f /etc/dorm-hub/compose.monitoring.yaml)
  fi
  APP_IMAGE="$image" docker compose --project-name dorm-hub --project-directory /opt/dorm-hub \
    --env-file /opt/dorm-hub/.env.production -f "$source_dir/compose.registry.yaml" config --quiet
  previous=$(mktemp /etc/dorm-hub/compose.before-search.XXXXXX)
  cp -a /etc/dorm-hub/compose.registry.yaml "$previous"
  install -m 600 "$source_dir/compose.registry.yaml" /etc/dorm-hub/compose.registry.yaml
  if APP_IMAGE="$image" "${compose[@]}" up -d --no-deps --no-build --pull missing \
       --wait --wait-timeout 120 meilisearch && \
     APP_IMAGE="$image" "${compose[@]}" up -d --no-deps --no-build --force-recreate --pull never \
       --wait --wait-timeout 120 app; then
    install -m 700 "$source_dir/server-deploy.sh" /usr/local/sbin/dorm-hub-deploy
    echo 'Meilisearch enabled. The index is being built in the background.'
    echo 'Database, photos and maintenance settings were retained.'
  else
    install -m 600 "$previous" /etc/dorm-hub/compose.registry.yaml
    APP_IMAGE="$image" "${compose[@]}" up -d --no-deps --no-build --force-recreate --pull never \
      --wait --wait-timeout 120 app || true
    echo 'Search installation failed; restored the previous Compose configuration.' >&2
    exit 1
  fi
  exit 0
fi

key=/root/.ssh/github_actions_deploy
if [[ ! -f "$key" ]]; then
  ssh-keygen -t ed25519 -f "$key" -C github-actions-deploy -N ''
fi
public_key=$(ssh-keygen -y -f "$key")
printf '%s github-actions-deploy\n' "$public_key" > "$key.pub"

# Keep a copy of configuration when re-running the installer.
if [[ -f /etc/dorm-hub/compose.registry.yaml ]]; then
  cp /etc/dorm-hub/compose.registry.yaml /etc/dorm-hub/compose.registry.yaml.previous
fi
APP_IMAGE=dorm-hub-app docker compose --project-name dorm-hub \
  --env-file /opt/dorm-hub/.env.production \
  -f "$source_dir/compose.registry.yaml" config --quiet
install -m 600 "$source_dir/compose.registry.yaml" /etc/dorm-hub/compose.registry.yaml
install -m 700 "$source_dir/server-deploy.sh" /usr/local/sbin/dorm-hub-deploy

authorized=/root/.ssh/authorized_keys
touch "$authorized"
chmod 600 "$authorized"
if ! grep -Fq -- "$public_key" "$authorized"; then
  # Start on a new line even if a previous key had no final newline.
  printf '\nrestrict,command="/usr/local/sbin/dorm-hub-deploy" %s github-actions-deploy\n' \
    "$public_key" >> "$authorized"
fi
echo 'Installed. Running containers have not been changed.'
echo 'Now add DEPLOY_SSH_KEY and DEPLOY_KNOWN_HOSTS to GitHub Actions secrets.'
