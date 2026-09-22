/*
 * Copyright (C) 2026 Intel Corporation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// scrapeMetrics gathers the metrics of the given exporter the same way the
// metrics endpoint does and returns the exposition format response body.
func scrapeMetrics(t *testing.T, exporter *Exporter) string {
	t.Helper()

	registry := prometheus.NewRegistry()
	if err := registry.Register(exporter); err != nil {
		t.Fatalf("cannot register the exporter: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, defaultMetricsURL, nil)

	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics endpoint returned %d: %s", recorder.Code, recorder.Body.String())
	}

	return recorder.Body.String()
}

func TestNewNetworkMetricsInfo(t *testing.T) {
	stats := &networkStatistics[0]

	info := newNetworkMetricsInfo("", "0A:0B:0C:0D:0E:0F", "eth_a", stats)

	desc := info.prometheusDesc.String()
	for _, want := range []string{
		defaultPrefix + stats.statisticsName,
		stats.statisticsDesc,
		`ifname="eth_a"`,
		`macaddr="0a:0b:0c:0d:0e:0f"`,
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description '%s' does not contain '%s'", desc, want)
		}
	}

	if strings.Contains(desc, "moduleid") {
		t.Errorf("description '%s' should not have a module id label", desc)
	}

	info = newNetworkMetricsInfo("42", "0A:0B:0C:0D:0E:0F", "eth_a", stats)

	if desc := info.prometheusDesc.String(); !strings.Contains(desc, `moduleid="42"`) {
		t.Errorf("description '%s' does not have the module id label", desc)
	}
}

func TestExporterDescribe(t *testing.T) {
	netConfs := l3NetworkConfigs()
	exporter := newExporter(netConfs)

	descs := make(chan *prometheus.Desc, len(networkStatistics)*len(netConfs)+1)
	exporter.Describe(descs)
	close(descs)

	unique := map[string]bool{}
	described := 0

	for desc := range descs {
		unique[desc.String()] = true
		described++
	}

	expected := len(networkStatistics) * len(netConfs)
	if described != expected {
		t.Errorf("expected %d descriptions, got %d", expected, described)
	}

	if len(unique) != expected {
		t.Errorf("expected %d unique descriptions, got %d", expected, len(unique))
	}
}

func TestExporterCollect(t *testing.T) {
	netConfs := l3NetworkConfigs()

	body := scrapeMetrics(t, newExporter(netConfs))

	collected := 0

	for _, line := range strings.Split(body, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			collected++
		}
	}

	expected := len(networkStatistics) * len(netConfs)
	if collected != expected {
		t.Errorf("expected %d metrics, got %d in:\n%s", expected, collected, body)
	}

	for ifname := range netConfs {
		if !strings.Contains(body, `ifname="`+ifname+`"`) {
			t.Errorf("no metrics for interface '%s' in:\n%s", ifname, body)
		}
	}
}

func TestExporterCollectValues(t *testing.T) {
	hwaddr := net.HardwareAddr{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	netConfs := map[string]*networkConfiguration{
		"eth_metrics": {
			moduleId:    "7",
			localHwAddr: &hwaddr,
		},
	}

	// the interface does not exist, so ethtool has no statistics for it and
	// all of the values are zero
	expected := `# HELP gaudi_scaleout_rx_bytes Bytes received by scale-out network
# TYPE gaudi_scaleout_rx_bytes counter
gaudi_scaleout_rx_bytes{ifname="eth_metrics",macaddr="0a:0b:0c:0d:0e:0f",moduleid="7"} 0
# HELP gaudi_scaleout_rx_errors Errors in scale-out network reception
# TYPE gaudi_scaleout_rx_errors counter
gaudi_scaleout_rx_errors{ifname="eth_metrics",macaddr="0a:0b:0c:0d:0e:0f",moduleid="7"} 0
# HELP gaudi_scaleout_rx_packets Packets received by scale-out network
# TYPE gaudi_scaleout_rx_packets counter
gaudi_scaleout_rx_packets{ifname="eth_metrics",macaddr="0a:0b:0c:0d:0e:0f",moduleid="7"} 0
# HELP gaudi_scaleout_tx_bytes Bytes transmitted by scale-out network
# TYPE gaudi_scaleout_tx_bytes counter
gaudi_scaleout_tx_bytes{ifname="eth_metrics",macaddr="0a:0b:0c:0d:0e:0f",moduleid="7"} 0
# HELP gaudi_scaleout_tx_errors Errors in scale-out network transmission
# TYPE gaudi_scaleout_tx_errors counter
gaudi_scaleout_tx_errors{ifname="eth_metrics",macaddr="0a:0b:0c:0d:0e:0f",moduleid="7"} 0
# HELP gaudi_scaleout_tx_packets Packets transmitted by scale-out network
# TYPE gaudi_scaleout_tx_packets counter
gaudi_scaleout_tx_packets{ifname="eth_metrics",macaddr="0a:0b:0c:0d:0e:0f",moduleid="7"} 0
`

	if body := scrapeMetrics(t, newExporter(netConfs)); body != expected {
		t.Errorf("expected metrics:\n%s\ngot:\n%s", expected, body)
	}
}

func TestStartMetricsServer(t *testing.T) {
	netConfs := l3NetworkConfigs()
	result := make(chan error, 1)

	// without an address no metrics server is started
	startMetricsServer(&cmdConfig{}, result, netConfs)

	select {
	case err := <-result:
		t.Errorf("no metrics server should have been started: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// an unusable port fails the metrics server
	startMetricsServer(&cmdConfig{metricsBindAddress: "127.0.0.1:-1"}, result, netConfs)

	select {
	case err := <-result:
		if err == nil {
			t.Error("metrics server should have failed for an invalid address")
		}
	case <-time.After(10 * time.Second):
		t.Error("metrics server did not report an error for an invalid address")
	}
}
