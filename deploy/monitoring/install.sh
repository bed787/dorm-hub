#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Run as root on the Linux VPS.' >&2; exit 1; }
[[ $(uname -s) == Linux ]] || { echo 'This installer is for the Linux VPS.' >&2; exit 1; }
root=/etc/dorm-hub-monitoring
if [[ ${1:-} == --show-login && $# == 1 ]]; then
  test -f "$root/secrets.env"
  printf 'Grafana user: admin\n'
  sed -n 's/^GRAFANA_ADMIN_PASSWORD=/Grafana password: /p' "$root/secrets.env"
  exit 0
fi
[[ $# == 0 ]] || { echo 'Usage: install.sh [--show-login]' >&2; exit 2; }
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
for cmd in docker openssl flock curl; do command -v "$cmd" >/dev/null; done
test -f /etc/dorm-hub/compose.registry.yaml
test -s /var/lib/dorm-hub-deploy/current-image
test -f /opt/dorm-hub/.env.production
test -f "$source_dir/../server-deploy.sh"
exec 9>/run/lock/dorm-hub-deploy.lock
flock -w 180 9
image=$(cat /var/lib/dorm-hub-deploy/current-image)
docker image inspect "$image" >/dev/null
base=(docker compose --project-name dorm-hub --project-directory /opt/dorm-hub
 --env-file /opt/dorm-hub/.env.production -f /etc/dorm-hub/compose.registry.yaml)
container=$(APP_IMAGE="$image" "${base[@]}" ps -q app)
[[ -n "$container" && "$container" != *$'\n'* ]] || { echo 'Expected one running app container.' >&2; exit 1; }
public_url=$(docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$container" | sed -n 's/^BASE_URL=//p')
[[ "$public_url" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] || { echo 'Expected an HTTPS BASE_URL in the running app.' >&2; exit 1; }
install -d -m 755 "$root"
if [[ ! -f "$root/secrets.env" ]]; then
  {
    printf 'GRAFANA_ADMIN_PASSWORD=%s\n' "$(openssl rand -hex 24)"
    printf 'GRAFANA_SECRET_KEY=%s\n' "$(openssl rand -hex 32)"
    printf 'MONITORING_PUBLIC_URL=%s\n' "$public_url"
  } > "$root/secrets.env"
  chmod 600 "$root/secrets.env"
fi
backup=$(mktemp -d "$root/backup.XXXXXX")
had_config=false; had_override=false
if [[ -d "$root/config" ]]; then cp -a "$root/config" "$backup/config"; had_config=true; fi
override=/etc/dorm-hub/compose.monitoring.yaml
if [[ -f "$override" ]]; then cp -a "$override" "$backup/app.override.yaml"; had_override=true; fi
install -d -m 755 "$root/config"
for file in compose.yaml prometheus.yml alerts.yml; do install -m 644 "$source_dir/$file" "$root/config/$file"; done
cp -R "$source_dir/grafana" "$root/config/"
find "$root/config" -type d -exec chmod 755 {} +
find "$root/config" -type f -exec chmod 644 {} +
monitor=(docker compose --project-name dorm-hub-monitoring --project-directory "$root/config"
 --env-file "$root/secrets.env" -f "$root/config/compose.yaml")
rollback() {
  result=$?
  trap - ERR
  if $had_override; then install -m 600 "$backup/app.override.yaml" "$override"; else rm -f "$override"; fi
  restore=("${base[@]}")
  if [[ -f "$override" ]]; then restore+=(-f "$override"); fi
  APP_IMAGE="$image" "${restore[@]}" up -d --no-deps --no-build --pull never --wait --wait-timeout 120 app || true
  if $had_config; then
    mv "$root/config" "$backup/failed-config"
    cp -a "$backup/config" "$root/config"
    "${monitor[@]}" up -d --force-recreate --wait --wait-timeout 120 || true
  else
    "${monitor[@]}" stop || true
  fi
  echo "Monitoring installation failed. Application configuration restored. Backup: $backup" >&2
  exit "$result"
}
trap rollback ERR
"${monitor[@]}" config --quiet
"${monitor[@]}" up -d --force-recreate --wait --wait-timeout 180
install -m 600 "$source_dir/app.override.yaml" "$override"
APP_IMAGE="$image" "${base[@]}" -f "$override" config --quiet
APP_IMAGE="$image" "${base[@]}" -f "$override" up -d --no-deps --no-build --pull never --wait --wait-timeout 120 app
container=$(APP_IMAGE="$image" "${base[@]}" -f "$override" ps -q app)
docker exec "$container" wget -q -O - http://127.0.0.1:9091/metrics | grep '^# HELP dormhub_' >/dev/null
install -m 700 "$source_dir/../server-deploy.sh" /usr/local/sbin/dorm-hub-deploy.next
mv -f /usr/local/sbin/dorm-hub-deploy.next /usr/local/sbin/dorm-hub-deploy
trap - ERR
printf 'Monitoring installed: %s/monitoring/\n' "$public_url"
printf 'Open Dorm Hub as administrator, then Statistics -> Server monitoring.\n'
printf 'To show the Grafana login: bash %q --show-login\n' "$source_dir/install.sh"
printf 'The application env file, database and photo volumes were retained.\n'
