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
	_ "embed"
	"testing"
)

// malformedYaml cannot be parsed as yaml at all: the flow sequence is never
// closed.
const (
	clusterRole   = "ClusterRole"
	malformedYaml = `
metadata:
  name: [unterminated
`
	// mistypedYaml parses as yaml, but the value types do not match the target
	// Kubernetes object.
	mistypedYaml = `
metadata: "this should be an object"
`
	dranetName             = "dranet"
	namespaceName          = "kube-system"
	imageName              = "registry.k8s.io/networking/dranet:stable"
	resourceName           = "dra.net"
	rolebindingName        = "linkdiscovery-openshift-privileged"
	rolebindingRolerefName = "system:openshift:scc:privileged"
	rolebindingAccount     = "linkdiscovery-sa"
	deviceClassName        = "dranet-rdma"
)

// expectPanic runs fn and fails the test unless fn panics.
func expectPanic(t *testing.T, fn func()) {
	t.Helper()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected a panic, got none")
		}
	}()

	fn()
}

//go:embed deployments/dranet/daemonset.yaml
var dsContent []byte

func TestGetDaemonSet(t *testing.T) {
	ds := GetDaemonSet(dsContent)
	if ds == nil {
		t.Fatal("expected to receive a valid daemonset")
	}

	if ds.Kind != "DaemonSet" {
		t.Errorf("expected kind to be 'DaemonSet', got: %s", ds.Kind)
	}

	if ds.Name != dranetName {
		t.Errorf("expected name to be '%s', got: %s", dranetName, ds.Name)
	}

	if ds.Namespace != namespaceName {
		t.Errorf("expected namespace to be '%s', got: %s", namespaceName, ds.Namespace)
	}

	if ds.Labels["app"] != dranetName {
		t.Errorf("expected label app to be '%s', got: %s", dranetName, ds.Labels["app"])
	}

	if !ds.Spec.Template.Spec.HostNetwork {
		t.Error("expected host network to be enabled")
	}

	if ds.Spec.Template.Spec.ServiceAccountName != dranetName {
		t.Errorf("expected service account name to be '%s', got: %s",
			dranetName, ds.Spec.Template.Spec.ServiceAccountName)
	}

	if len(ds.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got: %d", len(ds.Spec.Template.Spec.Containers))
	}

	if ds.Spec.Template.Spec.Containers[0].Name != dranetName {
		t.Errorf("expected container name to be '%s', got: %s",
			dranetName, ds.Spec.Template.Spec.Containers[0].Name)
	}

	if ds.Spec.Template.Spec.Containers[0].Image != imageName {
		t.Errorf("expected container image to be '%s', got: %s",
			imageName, ds.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestGetDaemonSetEmptyContent(t *testing.T) {
	ds := GetDaemonSet([]byte{})
	if ds == nil {
		t.Fatal("expected to receive an empty but valid daemonset")
	}

	if ds.Name != "" {
		t.Errorf("expected an empty name, got: %s", ds.Name)
	}
}

func TestGetDaemonSetInvalidContent(t *testing.T) {
	for name, content := range map[string]string{
		"malformed": malformedYaml,
		"mistyped":  mistypedYaml,
	} {
		t.Run(name, func(t *testing.T) {
			expectPanic(t, func() {
				GetDaemonSet([]byte(content))
			})
		})
	}
}

//go:embed deployments/dranet/serviceaccount.yaml
var saContent []byte

func TestGetServiceAccount(t *testing.T) {
	sa := GetServiceAccount(saContent)
	if sa == nil {
		t.Fatal("expected to receive a valid service account")
	}

	if sa.Kind != "ServiceAccount" {
		t.Errorf("expected kind to be 'ServiceAccount', got: %s", sa.Kind)
	}

	if sa.Name != dranetName {
		t.Errorf("expected name to be '%s', got: %s", dranetName, sa.Name)
	}

	if sa.Namespace != namespaceName {
		t.Errorf("expected namespace to be '%s', got: %s", namespaceName, sa.Namespace)
	}
}

func TestGetServiceAccountEmptyContent(t *testing.T) {
	sa := GetServiceAccount([]byte{})
	if sa == nil {
		t.Fatal("expected to receive an empty but valid service account")
	}

	if sa.Name != "" {
		t.Errorf("expected an empty name, got: %s", sa.Name)
	}
}

func TestGetServiceAccountInvalidContent(t *testing.T) {
	for name, content := range map[string]string{
		"malformed": malformedYaml,
		"mistyped":  mistypedYaml,
	} {
		t.Run(name, func(t *testing.T) {
			expectPanic(t, func() {
				GetServiceAccount([]byte(content))
			})
		})
	}
}

//go:embed deployments/dranet/clusterrole.yaml
var crContent []byte

func TestGetClusterRole(t *testing.T) {
	cr := GetClusterRole(crContent)
	if cr == nil {
		t.Fatal("expected to receive a valid cluster role")
	}

	if cr.Kind != clusterRole {
		t.Errorf("expected kind to be 'ClusterRole', got: %s", cr.Kind)
	}

	if cr.Name != dranetName {
		t.Errorf("expected name to be '%s', got: %s", dranetName, cr.Name)
	}

	if len(cr.Rules) != 6 {
		t.Fatalf("expected 6 rules, got: %d", len(cr.Rules))
	}

	if len(cr.Rules[0].Resources) != 1 || cr.Rules[0].Resources[0] != "nodes" {
		t.Errorf("expected the first rule to cover 'nodes', got: %v", cr.Rules[0].Resources)
	}

	if len(cr.Rules[0].Verbs) != 1 {
		t.Errorf("expected 1 verbs in the first rule, got: %v", cr.Rules[0].Verbs)
	}
}

func TestGetClusterRoleEmptyContent(t *testing.T) {
	cr := GetClusterRole([]byte{})
	if cr == nil {
		t.Fatal("expected to receive an empty but valid cluster role")
	}

	if len(cr.Rules) != 0 {
		t.Errorf("expected no rules, got: %d", len(cr.Rules))
	}
}

func TestGetClusterRoleInvalidContent(t *testing.T) {
	for name, content := range map[string]string{
		"malformed": malformedYaml,
		"mistyped":  mistypedYaml,
	} {
		t.Run(name, func(t *testing.T) {
			expectPanic(t, func() {
				GetClusterRole([]byte(content))
			})
		})
	}
}

//go:embed discovery/openshift/rolebinding.yaml
var osrbContent []byte

func TestGetRoleBinding(t *testing.T) {
	rb := GetRoleBinding(osrbContent)
	if rb == nil {
		t.Fatal("expected to receive a valid role binding")
	}

	if rb.Kind != "RoleBinding" {
		t.Errorf("expected kind to be 'RoleBinding', got: %s", rb.Kind)
	}

	if rb.Name != rolebindingName {
		t.Errorf("expected name to be '%s', got: %s", rolebindingName, rb.Name)
	}

	if rb.Namespace != "" {
		t.Errorf("expected namespace to be empty, got: %s", rb.Namespace)
	}

	if rb.RoleRef.Kind != clusterRole || rb.RoleRef.Name != rolebindingRolerefName {
		t.Errorf("expected the role ref to point to ClusterRole/%s, got: %s/%s",
			rolebindingName, rb.RoleRef.Kind, rb.RoleRef.Name)
	}

	if len(rb.Subjects) != 1 {
		t.Fatalf("expected 1 subject, got: %d", len(rb.Subjects))
	}

	if rb.Subjects[0].Kind != "ServiceAccount" || rb.Subjects[0].Name != rolebindingAccount {
		t.Errorf("expected the subject to be ServiceAccount/%s, got: %s/%s",
			rolebindingAccount, rb.Subjects[0].Kind, rb.Subjects[0].Name)
	}
}

func TestGetRoleBindingEmptyContent(t *testing.T) {
	rb := GetRoleBinding([]byte{})
	if rb == nil {
		t.Fatal("expected to receive an empty but valid role binding")
	}

	if len(rb.Subjects) != 0 {
		t.Errorf("expected no subjects, got: %d", len(rb.Subjects))
	}
}

func TestGetRoleBindingInvalidContent(t *testing.T) {
	for name, content := range map[string]string{
		"malformed": malformedYaml,
		"mistyped":  mistypedYaml,
	} {
		t.Run(name, func(t *testing.T) {
			expectPanic(t, func() {
				GetRoleBinding([]byte(content))
			})
		})
	}
}

//go:embed deployments/dranet/clusterrolebinding.yaml
var crbContent []byte

func TestGetClusterRoleBinding(t *testing.T) {
	crb := GetClusterRoleBinding(crbContent)
	if crb == nil {
		t.Fatal("expected to receive a valid cluster role binding")
	}

	if crb.Kind != "ClusterRoleBinding" {
		t.Errorf("expected kind to be 'ClusterRoleBinding', got: %s", crb.Kind)
	}

	if crb.Name != dranetName {
		t.Errorf("expected name to be '%s', got: %s", dranetName, crb.Name)
	}

	if crb.RoleRef.Kind != clusterRole || crb.RoleRef.Name != dranetName {
		t.Errorf("expected the role ref to point to ClusterRole/%s, got: %s/%s",
			dranetName, crb.RoleRef.Kind, crb.RoleRef.Name)
	}

	if len(crb.Subjects) != 1 {
		t.Fatalf("expected 1 subject, got: %d", len(crb.Subjects))
	}

	if crb.Subjects[0].Namespace != namespaceName {
		t.Errorf("expected the subject namespace to be '%s', got: %s",
			namespaceName, crb.Subjects[0].Namespace)
	}
}

func TestGetClusterRoleBindingEmptyContent(t *testing.T) {
	crb := GetClusterRoleBinding([]byte{})
	if crb == nil {
		t.Fatal("expected to receive an empty but valid cluster role binding")
	}

	if len(crb.Subjects) != 0 {
		t.Errorf("expected no subjects, got: %d", len(crb.Subjects))
	}
}

func TestGetClusterRoleBindingInvalidContent(t *testing.T) {
	for name, content := range map[string]string{
		"malformed": malformedYaml,
		"mistyped":  mistypedYaml,
	} {
		t.Run(name, func(t *testing.T) {
			expectPanic(t, func() {
				GetClusterRoleBinding([]byte(content))
			})
		})
	}
}

//go:embed deployments/dranet/rdmadeviceclass.yaml
var dcContent []byte

func TestGetDeviceClass(t *testing.T) {
	dc := GetDeviceClass(dcContent)
	if dc == nil {
		t.Fatal("expected to receive a valid device class")
	}

	if dc.Kind != "DeviceClass" {
		t.Errorf("expected kind to be 'DeviceClass', got: %s", dc.Kind)
	}

	if dc.Name != deviceClassName {
		t.Errorf("expected name to be '%s', got: %s", deviceClassName, dc.Name)
	}

	if len(dc.Spec.Selectors) != 2 {
		t.Fatalf("expected 2 selectors, got: %d", len(dc.Spec.Selectors))
	}

	for i, expected := range []string{
		`device.driver == "` + resourceName + `"`,
		`device.attributes["` + resourceName + `"].rdma == true`,
	} {
		cel := dc.Spec.Selectors[i].CEL
		if cel == nil {
			t.Errorf("expected selector %d to have a CEL selector", i)

			continue
		}

		if cel.Expression != expected {
			t.Errorf("expected selector %d expression to be %q, got: %q", i, expected, cel.Expression)
		}
	}
}

func TestGetDeviceClassEmptyContent(t *testing.T) {
	dc := GetDeviceClass([]byte{})
	if dc == nil {
		t.Fatal("expected to receive an empty but valid device class")
	}

	if len(dc.Spec.Selectors) != 0 {
		t.Errorf("expected no selectors, got: %d", len(dc.Spec.Selectors))
	}
}

func TestGetDeviceClassInvalidContent(t *testing.T) {
	for name, content := range map[string]string{
		"malformed": malformedYaml,
		"mistyped":  mistypedYaml,
	} {
		t.Run(name, func(t *testing.T) {
			expectPanic(t, func() {
				GetDeviceClass([]byte(content))
			})
		})
	}
}
