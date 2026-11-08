#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Run as root on the VPS.' >&2; exit 1; }
project=/opt/dorm-hub
folder=/etc/dorm-hub-monitoring/config/grafana/dashboards
if [[ ! -d "$folder" ]]; then
  echo 'Monitoring is not installed.' >&2
  exit 1
fi
# Read the fetched GitHub commit, without changing the application's worktree.
tmp=$(mktemp "$folder/.http-dashboard.XXXXXX")
trap 'rm -f "$tmp"' EXIT
git -C "$project" show origin/main:deploy/monitoring/grafana/dashboards/dorm-hub-http.json > "$tmp"
test -s "$tmp"
if command -v python3 >/dev/null; then python3 -m json.tool "$tmp" >/dev/null; fi
if [[ -f "$folder/dorm-hub-http.json" ]]; then cp -p "$folder/dorm-hub-http.json" "$folder/dorm-hub-http.json.previous"; fi
chmod 644 "$tmp"
mv -f "$tmp" "$folder/dorm-hub-http.json"
trap - EXIT
echo 'API dashboard installed. Grafana will load it automatically in about 10-30 seconds.'
echo 'Open Statistics -> API: requests and errors, or Dashboards -> Dorm Hub -> Dorm Hub - API.'
