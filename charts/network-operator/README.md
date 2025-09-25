# Helm install

Network operator can be installed via a helm chart. Most of its parameters can be modified with helm values.

The most important values are defined below:
|Name|Type|Default|
|---|---|---|
|config.gaudi.enabled|Install Gaudi CR alongside of the operator|false|
|config.gaudi.image.repository|Gaudi container repository path|intel/intel-network-linkdiscovery|
|config.gaudi.image.tag|Gaudi container repository tag|1.0.0|
|config.gaudi.mode|Gaudi operational mode, L2 or L3|L3|
|config.gaudi.mtu|MTU for the Gaudi network interfaces|8000|
|config.gaudi.pfc.config|Set Priority Flow Control (PFC) for the Gaudi network interfaces. "00000000" or "11110000"|"11110000"|
|config.gaudi.pfc.lldpad|Run LLDPAD inside the Pod. Otherwise container tries to access host's LLDPAD|false|
|config.gaudi.networkMetrics|Enable metrics from the Gaudi scale-out interfaces. Requires Prometheus in the cluster.|false|
|config.gaudi.networkManager.disable|If NM is running on host, try to disable it for the Gaudi network interfaces|false|
|nfd.install|Install NFD as part of the chart|false|
|nfd.gaudiRule|Install Gaudi NFD rules|true|
|operator.image.repository|Operator container image path|intel/intel-network-operator|
|operator.image.tag|Operator container image tag|1.0.0|
|logLevel|Log level for all entities|2|
|prometheus.labels|Label map to be used for the Prometheus ServiceMonitor|{"release": "prom"}|

See other values in the [values.yaml](values.yaml) file.

## Install

Install only operator without any custom resources:
```
helm install --namespace "intel-network-operator" --create-namespace --version 1.0.0 \
  network-operator oci://ghcr.io/intel/network-operator/intel-network-operator \
```

Install operator and a custom resource for Intel Gaudi:
```
helm install --namespace "intel-network-operator" --create-namespace --version 1.0.0 \
  network-operator oci://ghcr.io/intel/network-operator/intel-network-operator \
  --set config.gaudi.enabled=true --set config.gaudi.mode=L3 --set config.gaudi.mtu=9000
```

### LLDPAD

Install operator with Gaudi custom resources to hosts that have an LLDPAD service:
```
helm install --namespace "intel-network-operator" --create-namespace --version 1.0.0 \
  network-operator oci://ghcr.io/intel/network-operator/intel-network-operator \
  --set config.gaudi.enabled=true --set config.gaudi.mode=L3 --set config.gaudi.mtu=9000 \
  --set config.gaudi.pfc.config="11110000"
```

Install operator with Gaudi custom resources to hosts that do not have an LLDPAD service. LLDPAD runs within the Pod:
```
helm install --namespace "intel-network-operator" --create-namespace --version 1.0.0 \
  network-operator oci://ghcr.io/intel/network-operator/intel-network-operator \
  --set config.gaudi.enabled=true --set config.gaudi.mode=L3 --set config.gaudi.mtu=9000 \
  --set config.gaudi.pfc.config="11110000" --set config.gaudi.pfc.lldpad=true
```

### Metrics

Install operator with Gaudi custom resources and provide scale-out networking metrics for Prometheus
```
helm install --namespace "intel-network-operator" --create-namespace --version 1.0.0 \
  network-operator oci://ghcr.io/intel/network-operator/intel-network-operator \
  --set config.gaudi.enabled=true --set config.gaudi.mode=L3 --set config.gaudi.mtu=9000 \
  --set config.gaudi.networkMetrics=true --set prometheus.labels.release=prometheus
```

**Note:** `networkMetrics=true` requires Prometheus to be present in the cluster before installation, and  `prometheus.labels.release` value needs to match Prometheus Helm install release name.

## Uninstall

```
helm uninstall --namespace "intel-network-operator" network-operator
```
