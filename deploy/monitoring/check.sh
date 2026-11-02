#!/usr/bin/env bash
# CI-only checks. All credentials below are for a disposable local instance.
set -euo pipefail
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export GRAFANA_ADMIN_PASSWORD=integration-test-only-password
export GRAFANA_SECRET_KEY=integration-test-only-secret-key
export MONITORING_PUBLIC_URL=http://127.0.0.1:3300
fixture=$(mktemp)
container=""
cleanup(){
 rm -f "$fixture"
 if [[ -n "$container" ]];then docker rm -f "$container" >/dev/null 2>&1 || true;fi
}
trap cleanup EXIT
docker compose -f "$source_dir/compose.yaml" config --quiet
docker run --rm --entrypoint promtool --volume "$source_dir:/etc/prometheus:ro" \
 prom/prometheus:v3.15.1 check config /etc/prometheus/prometheus.yml
docker run --rm --entrypoint promtool --workdir /etc/prometheus --volume "$source_dir:/etc/prometheus:ro" \
 prom/prometheus:v3.15.1 test rules /etc/prometheus/alerts.test.yml
node "$source_dir/../../tests/http-promql.cjs" "$fixture"
chmod 644 "$fixture"
docker run --rm --entrypoint promtool --volume "$fixture:/tmp/http-tests.json:ro" \
 prom/prometheus:v3.15.1 test rules /tmp/http-tests.json
container="dormhub-grafana-check-${RANDOM}"
docker run -d --name "$container" --memory 512m --read-only \
 --publish 127.0.0.1:3300:3000 \
 --tmpfs /var/lib/grafana:rw,mode=1777,size=128m --tmpfs /tmp:rw,mode=1777,size=32m \
 --env GF_SECURITY_ADMIN_PASSWORD="$GRAFANA_ADMIN_PASSWORD" \
 --env GF_SECURITY_SECRET_KEY="$GRAFANA_SECRET_KEY" \
 --env GF_SERVER_ROOT_URL="$MONITORING_PUBLIC_URL/monitoring/" \
 --env GF_SERVER_SERVE_FROM_SUB_PATH=true \
 --env GF_PLUGINS_PREINSTALL_AUTO_UPDATE=false --env GF_ANALYTICS_REPORTING_ENABLED=false \
 --env GF_ANALYTICS_CHECK_FOR_UPDATES=false --env GF_SECURITY_CONTENT_SECURITY_POLICY=true \
 --volume "$source_dir/grafana/provisioning:/etc/grafana/provisioning:ro" \
 --volume "$source_dir/grafana/dashboards:/etc/grafana/dashboards:ro" \
 grafana/grafana:13.3.0 >/dev/null
ready=false
for ((attempt=0;attempt<90;attempt++));do
 if curl --fail --silent --max-time 2 "$MONITORING_PUBLIC_URL/monitoring/api/health" >/dev/null;then ready=true;break;fi
 sleep 1
done
if ! $ready;then docker logs --tail=80 "$container";exit 1;fi
for uid in dorm-hub dorm-hub-http;do
 curl --fail --silent --max-time 10 --user "admin:$GRAFANA_ADMIN_PASSWORD" \
  "$MONITORING_PUBLIC_URL/monitoring/api/dashboards/uid/$uid" | \
  python3 -c 'import json,sys; d=json.load(sys.stdin); assert len(d["dashboard"]["panels"])>=10'
done
curl --fail --silent --max-time 10 --user "admin:$GRAFANA_ADMIN_PASSWORD" \
 "$MONITORING_PUBLIC_URL/monitoring/api/plugins" | \
 python3 -c 'import json,sys; assert any(p["id"]=="prometheus" for p in json.load(sys.stdin))'
curl --fail --silent --max-time 10 --header 'Content-Type: application/json' \
 --header "Origin: $MONITORING_PUBLIC_URL" \
 --data '{"user":"admin","password":"integration-test-only-password"}' \
 "$MONITORING_PUBLIC_URL/monitoring/login" >/dev/null
echo 'Prometheus queries, Grafana login and both dashboards validated.'
