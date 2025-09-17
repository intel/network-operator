# Intel Network Operator for Kubernetes

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/intel/network-operator/badge)](https://scorecard.dev/viewer/?uri=github.com/intel/network-operator)

CAUTION: This is an beta / non-production software, do not use on production clusters.

Network Operator allows automatic configuring and easier use of RDMA NICs with Intel AI accelerators.

## Description

Network operator currently supports Gaudi and its integrated scale-out network interfaces.

### Intel® Gaudi®

Intel Gaudi and its integrated NICs are supported in two modes: L2 and L3.

Once configuration is done, the ready nodes will be labeled (via NFD) with `intel.feature.node.kubernetes.io/gaudi-scale-out=true`

#### L2

The L2 mode is where the scale-out interfaces are only brought up without IP addresses. The Gaudi FW will leverage the interfaces for scale-out operations without IPs. The scale-out network topology can be simple without L3 switching or routing protocols.

#### L3

The L3 mode refers to a scale-out network that has L3 switching enabled. The supported provisioning method for Intel Gaudi is a custom LLDP aided provisioning. It expects the LLDP to be configured on the switches with specific settings. For the IP provisioning, LLDP's `Port Description` field has to have the switch port's IP and netmask at the end of it. e.g. `no-alert 10.200.10.2/30`. The information is used to calculate the Gaudi NIC IP.

The operator will deploy configuration Pods to the worker nodes which will listen to the LLDP packets and then configure the node's network interfaces. In addition to the IP addresses for the Gaudi NICs, the configurator will also setup routes and create [configuration files](https://docs.habana.ai/en/v1.20.0/Management_and_Monitoring/Network_Configuration/Configure_E2E_Test_in_L3.html#generating-a-gaudinet-json-example) for the Gaudi SW to use. The configurator creates two routes for each NIC: 1) a route to `/30` point to point network, and 2) a route to `/16` larger network.

More info on the switch topology and configurations is available [here](https://docs.habana.ai/en/v1.20.0/Management_and_Monitoring/Network_Configuration/Configure_E2E_Test_in_L3.html).

### Future work

* Enable Host-NIC use in cluster
* Support to install Host-NIC KMD
* Configure RDMA NICs to be used with Intel AI accelerators

### Dependencies

The operator depends on following Kubernetes components:
* Intel Gaudi base operator
* Node Feature Discovery
* Cert-manager
* Prometheus (optional)

## Getting Started

### Prerequisites
- go version v1.23+
- docker version 17.03+.
- kubectl version v1.31+.
- Access to a Kubernetes v1.31+ cluster.

### Deploy operator using kubectl

Images are available at [dockerhub.io](https://hub.docker.com/r/intel/intel-network-operator).

**Install NFD Gaudi device rules into the cluster:**

```sh
kubectl apply -f config/nfd/gaudi-device-rule.yaml
```

**Install operator into the cluster:**

```sh
kubectl apply -k config/operator/default/
```

**Create instances of your solution**

Ensure that the samples have desired [operator configuration values](#operator-configuration)
from the configuration options below. After that apply for example a Gaudi L3
sample with:

```sh
kubectl apply -f config/operator/samples/gaudi-l3.yaml
```

### Remove operator using kubectl

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -f config/operator/samples/gaudi-l3.yaml
```

**Uninstall the controller from the cluster:**

```sh
kubectl delete -k config/operator/default/
```

**Remove NFD Gaudi device rules from the cluster:**
```sh
kubectl delete -f config/nfd/gaudi-device-rule.yaml
```

### Deploy and remove Operator using Helm

See the [README for Helm installation](charts/network-operator/README.md).

### Prometheus scale-out network metrics

In order to supply scale-out network metrics to Prometheus, enable them
in the CR by setting `networkMetrics=true`. Use for example the
[gaudi-l3-metrics](config/operator/samples/gaudi-l3-metrics.yaml) sample
configuration.

```sh
kubectl apply -f config/operator/samples/gaudi-l3-metrics.yaml
```

This enables scale-out metrics at port `50152` and URL path `/metrics`.

Sample can be removed using kubectl.
```sh
kubectl delete -f config/operator/samples/gaudi-l3-metrics.yaml
```

**Enable metrics Service and Prometheus ServiceMonitor for it**

```sh
kubectl apply -f config/discovery/prometheus/metrics-service.yaml
```

Prometheus needs to be installed for the following kubectl command to succeed.

```sh
kubectl apply -f config/discovery/prometheus/monitor.yaml
```

**Disable metrics Service and its Prometheus ServiceMonitor**

Prometheus support can be removed using kubectl.
```sh
kubectl delete -f config/discovery/prometheus/monitor.yaml
kubectl delete -f config/discovery/prometheus/metrics-service.yaml
```

## Operator configuration

The most important Network Operator CRD properties are:

* `disableNetworkManager` boolean

    Disable Gaudi scale-out interfaces in NetworkManager. For nodes where NetworkManager tries
    to configure the Gaudi interfaces, prevent it from doing so.

* `enableLLDPAD` boolean

    Enable LLDP for Priority Flow Control in a dedicated container.
    Keep this value as `false` if lldpad LLDP daemon is already present and
    running on the host.

* `layer` enum

    Link layer where the scale-out communication should occur. Possible options are `L2` and `L3`.

* `mtu` integer

    description: MTU for the scale-out interfaces. Maximum `9000`, minimum `1500`.

* `pfcPriorities` string

    Bitmask of Priority Flow Control priorities to enable. Requires 'lldpad' on the host
    or enabled in a container with the above `enableLLDPAD` boolean. Currently the only two
    accepted values are `00000000` and `11110000`.

The full set of properties is available in the [NetworkClusterPolicy CRD definition](config/operator/crd/bases/intel.com_networkclusterpolicies.yaml).
Examples of Network Operator CRDs are found in the [samples directory](config/operator/samples/).

## Contributing

[Contributions](CONTRIBUTING.md) to this project are welcome as issues (bugs, enhancement requests) or via pull requests. Please review our [Code of Conduct](CODE_OF_CONDUCT.md) and our note on [security policy](SECURITY.md).

## License

Copyright 2024 Intel Corporation. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

##

Intel, the Intel logo and Gaudi are trademarks of Intel Corporation or its subsidiaries.
