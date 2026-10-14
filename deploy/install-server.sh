#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Run this installer as root on the VPS.' >&2; exit 1; }
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
test -f /opt/dorm-hub/.env.production
command -v docker >/dev/null
command -v flock >/dev/null
docker compose version
mkdir -p /etc/dorm-hub /var/lib/dorm-hub-deploy /root/.ssh
chmod 700 /root/.ssh

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
