/*
 * Copyright (C) 2025 Intel Corporation
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
	"net/http"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/safchain/ethtool"
	"k8s.io/klog/v2"
)

const (
	defaultPrefix     = "gaudi_scaleout_"
	defaultMetricsURL = "/metrics"
)

type ethStats struct {
	statisticsName  string
	statisticsDesc  string
	prometheusType  prometheus.ValueType
	statisticsSumOf []string
}

type networkMetricsInfo struct {
	stats          *ethStats
	prometheusDesc *prometheus.Desc
}

var (
	networkStatistics = []ethStats{
		{"rx_packets", "Packets received by scale-out network", prometheus.CounterValue,
			[]string{"ifInUcastPkts", "ifInMulticastPkts", "ifInBroadcastPkts"}},
		{"tx_packets", "Packets transmitted by scale-out network", prometheus.CounterValue,
			[]string{"ifOutUcastPkts", "ifOutMulticastPkts", "ifOutBroadcastPkts"}},
		{"rx_bytes", "Bytes received by scale-out network", prometheus.CounterValue,
			[]string{"OctetsReceivedOK"}},
		{"tx_bytes", "Bytes transmitted by scale-out network", prometheus.CounterValue,
			[]string{"OctetsTransmittedOK"}},
		{"rx_errors", "Errors in scale-out network reception", prometheus.CounterValue,
			[]string{"ifInErrors"}},
		{"tx_errors", "Errors in scale-out network transmission", prometheus.CounterValue,
			[]string{"ifOutErrors"}},
	}
)

func newNetworkMetricsInfo(macaddr string, ifname string, stats *ethStats) networkMetricsInfo {
	return networkMetricsInfo{
		stats: stats,
		prometheusDesc: prometheus.NewDesc(
			defaultPrefix+stats.statisticsName,
			stats.statisticsDesc,
			nil,
			prometheus.Labels{"macaddr": strings.ToLower(macaddr), "ifname": ifname},
		),
	}
}

type Exporter struct {
	mutex   sync.RWMutex
	metrics map[string][]networkMetricsInfo
}

func newExporter(networkConfigs map[string]*networkConfiguration) *Exporter {
	e := Exporter{
		metrics: make(map[string][]networkMetricsInfo),
	}

	for ifname, nwconfig := range networkConfigs {
		var metricsInfo []networkMetricsInfo

		for _, stats := range networkStatistics {
			metricsInfo = append(metricsInfo, newNetworkMetricsInfo(
				nwconfig.localHwAddr.String(),
				ifname,
				&stats))
		}

		e.metrics[ifname] = metricsInfo
	}

	return &e
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range e.metrics {
		for _, i := range m {
			ch <- i.prometheusDesc
		}
	}
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	for ifname, metrics := range e.metrics {
		// stats are all zero on error or missing values
		ethstats, err := ethtool.Stats(ifname)
		if err != nil {
			klog.Warningf("ethtool statistics for '%s' failed: %v", ifname, err)
		}

		for _, metricsinfo := range metrics {
			sum := uint64(0)
			for _, v := range metricsinfo.stats.statisticsSumOf {
				sum += ethstats[v]
			}
			ch <- prometheus.MustNewConstMetric(metricsinfo.prometheusDesc,
				metricsinfo.stats.prometheusType,
				float64(sum))
		}
	}
}

func startMetricsServer(config *cmdConfig, res chan<- error, networkConfigs map[string]*networkConfiguration) {
	if config.metricsBindAddress == "" {
		return
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(newExporter(networkConfigs))

	server := http.NewServeMux()
	server.Handle(defaultMetricsURL, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	go func(server *http.ServeMux, hostPort string, res chan<- error) {
		klog.Infof("Enabled metrics endpoint '%s'", hostPort)
		res <- http.ListenAndServe(hostPort, server)
	}(server, config.metricsBindAddress, res)
}
