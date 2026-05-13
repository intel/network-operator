// Copyright 2024 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deployments

import (
	_ "embed"

	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	helpers "github.com/intel/network-operator/config"
)

//go:embed base/daemonset.yaml
var contentGaudiDiscoveryDs []byte

//go:embed generic/linkdiscovery-serviceaccount.yaml
var contentLinkDiscoveryServiceAccount []byte

//go:embed openshift/rolebinding.yaml
var contentOpenshiftRoleBinding []byte

//go:embed base/lldpad-container.yaml
var contentLLDPADContainer []byte

func GaudiDiscoveryDaemonSet() *apps.DaemonSet {
	return helpers.GetDaemonSet(contentGaudiDiscoveryDs).DeepCopy()
}

func GaudiLinkDiscoveryServiceAccount() *core.ServiceAccount {
	return helpers.GetServiceAccount(contentLinkDiscoveryServiceAccount).DeepCopy()
}

func OpenShiftRoleBinding() *rbac.RoleBinding {
	return helpers.GetRoleBinding(contentOpenshiftRoleBinding).DeepCopy()
}

func LLDPADContainer() *core.Container {
	var result core.Container

	err := yaml.Unmarshal(contentLLDPADContainer, &result)
	if err != nil {
		panic(err)
	}

	return &result
}
