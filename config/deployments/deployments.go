// Copyright 2025,2026 Intel Corporation. All Rights Reserved.
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
)

//go:embed dranet/clusterrole.yaml
var contentClusterRole []byte

func DranetClusterRole() *rbac.ClusterRole {
	return getClusterRole(contentClusterRole).DeepCopy()
}

//go:embed dranet/clusterrolebinding.yaml
var contentClusterRoleBinding []byte

func DranetClusterRoleBinding() *rbac.ClusterRoleBinding {
	return getClusterRoleBinding(contentClusterRoleBinding).DeepCopy()
}

//go:embed dranet/serviceaccount.yaml
var contentServiceAccount []byte

func DranetServiceAccount() *core.ServiceAccount {
	return getServiceAccount(contentServiceAccount).DeepCopy()
}

//go:embed dranet/daemonset.yaml
var contentDaemonSet []byte

func DranetDaemonSet() *apps.DaemonSet {
	return getDaemonSet(contentDaemonSet).DeepCopy()
}

func getClusterRole(content []byte) *rbac.ClusterRole {
	var result rbac.ClusterRole

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

func getClusterRoleBinding(content []byte) *rbac.ClusterRoleBinding {
	var result rbac.ClusterRoleBinding

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

func getServiceAccount(content []byte) *core.ServiceAccount {
	var result core.ServiceAccount

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

func getDaemonSet(content []byte) *apps.DaemonSet {
	var result apps.DaemonSet

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}
