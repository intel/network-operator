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

	helpers "github.com/intel/network-operator/config"
)

//go:embed dranet/clusterrole.yaml
var contentClusterRole []byte

func DranetClusterRole() *rbac.ClusterRole {
	return helpers.GetClusterRole(contentClusterRole).DeepCopy()
}

//go:embed dranet/clusterrolebinding.yaml
var contentClusterRoleBinding []byte

func DranetClusterRoleBinding() *rbac.ClusterRoleBinding {
	return helpers.GetClusterRoleBinding(contentClusterRoleBinding).DeepCopy()
}

//go:embed dranet/serviceaccount.yaml
var contentServiceAccount []byte

func DranetServiceAccount() *core.ServiceAccount {
	return helpers.GetServiceAccount(contentServiceAccount).DeepCopy()
}

//go:embed dranet/daemonset.yaml
var contentDaemonSet []byte

func DranetDaemonSet() *apps.DaemonSet {
	return helpers.GetDaemonSet(contentDaemonSet).DeepCopy()
}
