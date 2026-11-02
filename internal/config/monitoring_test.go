package config

import "testing"

func TestMonitoringConfigIsOptionalAndValidatesOrigins(t *testing.T) {
	defaults, err := load(settings(nil))
	if err != nil || defaults.MetricsAddr != "" || defaults.MonitoringURL != "" {
		t.Fatal("monitoring must be optional", err)
	}
	valid, err := load(settings(map[string]string{"METRICS_ADDR": "0.0.0.0:9091", "MONITORING_URL": "http://dorm-hub-grafana:3000"}))
	if err != nil || valid.MonitoringURL == "" {
		t.Fatal(err)
	}
	for _, values := range []map[string]string{{"METRICS_ADDR": "9091"}, {"METRICS_ADDR": "localhost:0"}, {"MONITORING_URL": "http://user:password@host"}, {"MONITORING_URL": "http://host/path"}, {"MONITORING_URL": "http://host?secret=value"}} {
		if _, err = load(settings(values)); err == nil {
			t.Fatal("invalid monitoring config accepted", values)
		}
	}
}
