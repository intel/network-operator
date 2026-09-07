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

package deployments

import (
	"testing"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	resource "k8s.io/api/resource/v1"
)

const (
	mutated         = "mutated"
	dranetName      = "dranet"
	dranetNamespace = "kube-system"
	dranetDriver    = "dra.net"
)

func TestDranetClusterRole(t *testing.T) {
	cr := DranetClusterRole()
	if cr == nil {
		t.Fatal("expected to receive a valid cluster role")
	}

	if cr.Name != dranetName {
		t.Errorf("expected name to be %q, got: %s", dranetName, cr.Name)
	}

	if len(cr.Rules) == 0 {
		t.Fatal("expected the cluster role to contain rules")
	}

	// The driver needs to be able to publish its resource slices and to
	// update the status of the claims it handles.
	needed := map[string][]string{
		"resourceslices":        {"list", "watch", "create", "update", "delete"},
		"resourceclaims/status": {"patch", "update"},
		"nodes":                 {"get"},
	}

	for res, verbs := range needed {
		granted := grantedVerbs(cr.Rules, res)
		for _, verb := range verbs {
			if !granted[verb] {
				t.Errorf("expected the cluster role to grant %q on %q, got: %v",
					verb, res, granted)
			}
		}
	}
}

// grantedVerbs collects the set of verbs granted on the given resource across
// all policy rules.
func grantedVerbs(rules []rbac.PolicyRule, name string) map[string]bool {
	granted := map[string]bool{}

	for _, rule := range rules {
		for _, res := range rule.Resources {
			if res != name {
				continue
			}

			for _, verb := range rule.Verbs {
				granted[verb] = true
			}
		}
	}

	return granted
}

func TestDranetClusterRoleBinding(t *testing.T) {
	crb := DranetClusterRoleBinding()
	if crb == nil {
		t.Fatal("expected to receive a valid cluster role binding")
	}

	if crb.Name != dranetName {
		t.Errorf("expected name to be %q, got: %s", dranetName, crb.Name)
	}

	if crb.RoleRef.Kind != "ClusterRole" {
		t.Errorf("expected the role ref kind to be 'ClusterRole', got: %s", crb.RoleRef.Kind)
	}

	// The binding has to reference the cluster role shipped alongside it.
	if crb.RoleRef.Name != DranetClusterRole().Name {
		t.Errorf("expected the role ref to name %q, got: %s",
			DranetClusterRole().Name, crb.RoleRef.Name)
	}

	if len(crb.Subjects) != 1 {
		t.Fatalf("expected 1 subject, got: %d", len(crb.Subjects))
	}

	sa := DranetServiceAccount()

	subject := crb.Subjects[0]
	if subject.Kind != "ServiceAccount" {
		t.Errorf("expected the subject kind to be 'ServiceAccount', got: %s", subject.Kind)
	}

	// The binding has to reference the service account shipped alongside it.
	if subject.Name != sa.Name || subject.Namespace != sa.Namespace {
		t.Errorf("expected the subject to be %s/%s, got: %s/%s",
			sa.Namespace, sa.Name, subject.Namespace, subject.Name)
	}
}

func TestDranetServiceAccount(t *testing.T) {
	sa := DranetServiceAccount()
	if sa == nil {
		t.Fatal("expected to receive a valid service account")
	}

	if sa.Name != dranetName {
		t.Errorf("expected name to be %q, got: %s", dranetName, sa.Name)
	}

	if sa.Namespace != dranetNamespace {
		t.Errorf("expected namespace to be %q, got: %s", dranetNamespace, sa.Namespace)
	}
}

func TestDranetDaemonSet(t *testing.T) {
	ds := DranetDaemonSet()
	if ds == nil {
		t.Fatal("expected to receive a valid daemonset")
	}

	if ds.Name != dranetName {
		t.Errorf("expected name to be %q, got: %s", dranetName, ds.Name)
	}

	if ds.Namespace != dranetNamespace {
		t.Errorf("expected namespace to be %q, got: %s", dranetNamespace, ds.Namespace)
	}

	if ds.Spec.Selector == nil {
		t.Fatal("expected the daemonset to have a pod selector")
	}

	// The selector must match the labels of the pod template, otherwise the
	// API server rejects the daemonset.
	for key, value := range ds.Spec.Selector.MatchLabels {
		if ds.Spec.Template.Labels[key] != value {
			t.Errorf("expected the pod template label %q to be %q, got: %q",
				key, value, ds.Spec.Template.Labels[key])
		}
	}

	podSpec := ds.Spec.Template.Spec

	if !podSpec.HostNetwork {
		t.Error("expected host network to be enabled")
	}

	if podSpec.ServiceAccountName != DranetServiceAccount().Name {
		t.Errorf("expected service account name to be %q, got: %s",
			DranetServiceAccount().Name, podSpec.ServiceAccountName)
	}

	if len(podSpec.Containers) != 1 {
		t.Fatalf("expected 1 container, got: %d", len(podSpec.Containers))
	}

	container := podSpec.Containers[0]

	if container.Name != dranetName {
		t.Errorf("expected container name to be %q, got: %s", dranetName, container.Name)
	}

	if container.Image == "" {
		t.Error("expected the container to define an image")
	}

	// The driver requires privileges to set up network interfaces.
	if container.SecurityContext == nil || container.SecurityContext.Privileged == nil ||
		!*container.SecurityContext.Privileged {
		t.Error("expected the container to be privileged")
	}

	// The node name is passed in as an environment variable and referenced
	// from the command line arguments.
	nodeName := findEnv(container.Env, "NODE_NAME")
	if nodeName == nil {
		t.Error("expected the container to define the NODE_NAME environment variable")
	} else if nodeName.ValueFrom == nil || nodeName.ValueFrom.FieldRef == nil ||
		nodeName.ValueFrom.FieldRef.FieldPath != "spec.nodeName" {
		t.Error("expected NODE_NAME to be read from the spec.nodeName field")
	}

	args := map[string]bool{
		"/dranet":                             true,
		"--v=4":                               true,
		"--hostname-override=$(NODE_NAME)":    true,
		"--kubelet-root-dir=/var/lib/kubelet": true,
	}

	if len(container.Args) == 0 {
		t.Error("expected the container to define arguments")
	}

	for _, argval := range container.Args {
		if _, exists := args[argval]; !exists {
			t.Error("extra container argument(s)", argval)
		}
		delete(args, argval)
	}

	if len(args) != 0 {
		t.Error("expected arguments", args, "not found")
	}

	// Every volume mount has to have a matching volume.
	volumes := map[string]bool{}
	for _, volume := range podSpec.Volumes {
		volumes[volume.Name] = true
	}

	for _, mount := range container.VolumeMounts {
		if !volumes[mount.Name] {
			t.Errorf("volume mount %q has no matching volume", mount.Name)
		}
	}

	if len(podSpec.Tolerations) == 0 {
		t.Error("expected the daemonset to tolerate node taints")
	}
}

// findEnv returns the named environment variable, or nil when it is not found.
func findEnv(env []core.EnvVar, name string) *core.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}

	return nil
}

func TestDranetRDMADeviceClass(t *testing.T) {
	dc := DranetRDMADeviceClass()
	if dc == nil {
		t.Fatal("expected to receive a valid device class")
	}

	if dc.Name != "dranet-rdma" {
		t.Errorf("expected name to be 'dranet-rdma', got: %s", dc.Name)
	}

	if len(dc.Spec.Selectors) == 0 {
		t.Fatal("expected the device class to contain selectors")
	}

	for i, selector := range dc.Spec.Selectors {
		if selector.CEL == nil {
			t.Errorf("expected selector %d to have a CEL selector", i)

			continue
		}

		if selector.CEL.Expression == "" {
			t.Errorf("expected selector %d to have a non-empty expression", i)
		}
	}

	// The device class must limit itself to the DRAnet driver, and pick RDMA
	// capable devices only.
	if !containsExpression(dc, `device.driver == "`+dranetDriver+`"`) {
		t.Errorf("expected a selector matching the %q driver", dranetDriver)
	}

	if !containsExpression(dc, `device.attributes["`+dranetDriver+`"].rdma == true`) {
		t.Error("expected a selector matching RDMA capable devices")
	}
}

// containsExpression reports whether the device class has a CEL selector with
// the given expression.
func containsExpression(dc *resource.DeviceClass, expression string) bool {
	for _, selector := range dc.Spec.Selectors {
		if selector.CEL != nil && selector.CEL.Expression == expression {
			return true
		}
	}

	return false
}

// TestDeepCopy verifies that the accessors hand out independent copies, so that
// a caller mutating one of them does not corrupt the embedded content for the
// next caller.
func TestDeepCopy(t *testing.T) {
	t.Run("clusterrole", func(t *testing.T) {
		first := DranetClusterRole()
		first.Name = mutated
		first.Rules = nil

		second := DranetClusterRole()
		if second.Name != dranetName {
			t.Errorf("expected name to be %q, got: %s", dranetName, second.Name)
		}

		if len(second.Rules) == 0 {
			t.Error("expected the rules to be intact")
		}
	})

	t.Run("clusterrolebinding", func(t *testing.T) {
		first := DranetClusterRoleBinding()
		first.Name = mutated
		first.Subjects = nil

		second := DranetClusterRoleBinding()
		if second.Name != dranetName {
			t.Errorf("expected name to be %q, got: %s", dranetName, second.Name)
		}

		if len(second.Subjects) == 0 {
			t.Error("expected the subjects to be intact")
		}
	})

	t.Run("serviceaccount", func(t *testing.T) {
		first := DranetServiceAccount()
		first.Name = mutated

		second := DranetServiceAccount()
		if second.Name != dranetName {
			t.Errorf("expected name to be %q, got: %s", dranetName, second.Name)
		}
	})

	t.Run("daemonset", func(t *testing.T) {
		first := DranetDaemonSet()
		first.Name = mutated
		first.Spec.Template.Spec.Containers[0].Image = "mutated:latest"

		second := DranetDaemonSet()
		if second.Name != dranetName {
			t.Errorf("expected name to be %q, got: %s", dranetName, second.Name)
		}

		if second.Spec.Template.Spec.Containers[0].Image == "mutated:latest" {
			t.Error("expected the container image to be intact")
		}
	})

	t.Run("deviceclass", func(t *testing.T) {
		first := DranetRDMADeviceClass()
		first.Name = mutated
		first.Spec.Selectors = nil

		second := DranetRDMADeviceClass()
		if second.Name != "dranet-rdma" {
			t.Errorf("expected name to be 'dranet-rdma', got: %s", second.Name)
		}

		if len(second.Spec.Selectors) == 0 {
			t.Error("expected the selectors to be intact")
		}
	})
}
