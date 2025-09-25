// Copyright 2025 Intel Corporation. All Rights Reserved.
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
)

func TestGetDaemonset(t *testing.T) {
	ds := GaudiDiscoveryDaemonSet()
	if ds == nil {
		t.Error("expected to receive a valid daemonset")
	}
}

func TestGaudiServiceAccount(t *testing.T) {
	sa := GaudiLinkDiscoveryServiceAccount()
	if sa == nil {
		t.Error("expected to receive a valid service account")
	}
}

func TestOpenShiftRoleBinding(t *testing.T) {
	rb := OpenShiftRoleBinding()
	if rb == nil {
		t.Error("expected to receive a valid role binding")
	}
}

func TestLLDPADContainer(t *testing.T) {
	c := LLDPADContainer()
	if c != nil {
		if c.Name != "lldpad" {
			t.Errorf("expected container name to be 'lldpad', got: %s", c.Name)
		}
	} else {
		t.Error("expected to receive a valid container")
	}
}
