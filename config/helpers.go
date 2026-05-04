// Copyright 2026 Intel Corporation. All Rights Reserved.
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

package helpers

import (
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	resource "k8s.io/api/resource/v1"
	"sigs.k8s.io/yaml"
)

// GetDaemonSet unmarshals yaml content into a DaemonSet object.
func GetDaemonSet(content []byte) *apps.DaemonSet {
	var result apps.DaemonSet

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

// GetServiceAccount unmarshals yaml content into a ServiceAccount object.
func GetServiceAccount(content []byte) *core.ServiceAccount {
	var result core.ServiceAccount

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

func GetClusterRole(content []byte) *rbac.ClusterRole {
	var result rbac.ClusterRole

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

// GetRoleBinding unmarshals yaml content into a RoleBinding object.
func GetRoleBinding(content []byte) *rbac.RoleBinding {
	var result rbac.RoleBinding

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

func GetClusterRoleBinding(content []byte) *rbac.ClusterRoleBinding {
	var result rbac.ClusterRoleBinding

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}

func GetDeviceClass(content []byte) *resource.DeviceClass {
	var result resource.DeviceClass

	err := yaml.Unmarshal(content, &result)
	if err != nil {
		panic(err)
	}

	return &result
}
