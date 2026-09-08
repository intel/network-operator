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

package main

import (
	"flag"
	"io"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// testFlagSet returns a flag set that reports parse errors instead of exiting
// the test binary, and that keeps usage output away from the test log.
func testFlagSet(t *testing.T) *flag.FlagSet {
	t.Helper()

	fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	return fs
}

func TestParseFlagsDefaults(t *testing.T) {
	opts, err := parseFlags(testFlagSet(t), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Metrics are disabled by default and only enabled through the operator
	// deployment.
	if opts.metricsAddr != "0" {
		t.Errorf("expected the metrics address to default to '0', got: %s", opts.metricsAddr)
	}

	if opts.probeAddr != ":8081" {
		t.Errorf("expected the probe address to default to ':8081', got: %s", opts.probeAddr)
	}

	if opts.enableLeaderElection {
		t.Error("expected leader election to be disabled by default")
	}

	if opts.secureMetrics {
		t.Error("expected secure metrics serving to be disabled by default")
	}

	// HTTP/2 stays off by default because of the Stream Cancellation and Rapid
	// Reset CVEs.
	if opts.enableHTTP2 {
		t.Error("expected http/2 to be disabled by default")
	}

	if !opts.zapOptions.Development {
		t.Error("expected the zap logger to default to development mode")
	}
}

func TestParseFlagsOverrides(t *testing.T) {
	args := []string{
		"-metrics-bind-address=:8443",
		"-health-probe-bind-address=:9090",
		"-leader-elect",
		"-metrics-secure",
		"-enable-http2",
	}

	opts, err := parseFlags(testFlagSet(t), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.metricsAddr != ":8443" {
		t.Errorf("expected the metrics address to be ':8443', got: %s", opts.metricsAddr)
	}

	if opts.probeAddr != ":9090" {
		t.Errorf("expected the probe address to be ':9090', got: %s", opts.probeAddr)
	}

	if !opts.enableLeaderElection {
		t.Error("expected leader election to be enabled")
	}

	if !opts.secureMetrics {
		t.Error("expected secure metrics serving to be enabled")
	}

	if !opts.enableHTTP2 {
		t.Error("expected http/2 to be enabled")
	}
}

func TestParseFlagsInvalid(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag":  {"-no-such-flag"},
		"missing value": {"-metrics-bind-address"},
		"non boolean":   {"-leader-elect=maybe"},
		"help":          {"-help"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFlags(testFlagSet(t), args); err == nil {
				t.Errorf("expected an error for args %v", args)
			}
		})
	}
}

// TestParseFlagsExternalFlags verifies that the logging flags of the zap and
// klog libraries are registered on the flag set handed to parseFlags, and not
// on the global one.
func TestParseFlagsExternalFlags(t *testing.T) {
	fs := testFlagSet(t)

	if _, err := parseFlags(fs, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"zap-devel", "zap-log-level", "v"} {
		if fs.Lookup(name) == nil {
			t.Errorf("expected the %q flag to be registered", name)
		}
	}
}

// TestSchemeRegistration verifies that the scheme handed to the manager knows
// both the built in Kubernetes types the operator deploys and its own custom
// resource.
func TestSchemeRegistration(t *testing.T) {
	for _, gvk := range []schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "Pod"},
		{Group: "", Version: "v1", Kind: "ServiceAccount"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "intel.com", Version: "v1alpha1", Kind: "NetworkClusterPolicy"},
	} {
		if !scheme.Recognizes(gvk) {
			t.Errorf("expected the scheme to recognize %s", gvk)
		}
	}
}
