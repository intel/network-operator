# Intel Network Operator for Kubernetes

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/intel/network-operator/badge)](https://scorecard.dev/viewer/?uri=github.com/intel/network-operator)

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

## Getting Started

### Prerequisites
- go version v1.23+
- docker version 17.03+.
- kubectl version v1.31+.
- Access to a Kubernetes v1.31+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
IMG=<some-registry>/intel-network-operator TAG=<some-tag> make operator-image operator-push
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
kubectl apply -k config/operator/default/
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/intel-network-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

> **NOTE**: This doesn't seem to work?.

**Create instances of your solution**
You can apply a Gaudi L3 example:

```sh
kubectl apply -f config/operator/samples/gaudi-l3.yaml
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -f config/operator/samples/gaudi-l3.yaml
```

**UnDeploy the controller from the cluster:**

```sh
kubectl delete -k config/operator/default/
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/intel-network-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/intel/network-operator/<tag or branch>/dist/install.yaml
```

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
