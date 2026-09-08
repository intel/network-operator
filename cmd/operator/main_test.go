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
	"crypto/tls"
	"flag"
	"io"
	"slices"
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

// applyTLSOptions runs every TLS option in order and returns the resulting
// configuration.
func applyTLSOptions(opts []func(*tls.Config)) *tls.Config {
	cfg := &tls.Config{} //nolint:gosec // the options under test set the minimum version

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

func TestTLSOptions(t *testing.T) {
	expectedCiphers := []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	}

	for name, enableHTTP2 := range map[string]bool{
		"http/2 disabled": false,
		"http/2 enabled":  true,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := applyTLSOptions(tlsOptions(enableHTTP2))

			if cfg.MinVersion != tls.VersionTLS12 {
				t.Errorf("expected the minimum TLS version to be TLS 1.2, got: %#04x", cfg.MinVersion)
			}

			if cfg.MaxVersion != tls.VersionTLS12 {
				t.Errorf("expected the maximum TLS version to be TLS 1.2, got: %#04x", cfg.MaxVersion)
			}

			if !slices.Equal(cfg.CipherSuites, expectedCiphers) {
				t.Errorf("expected cipher suites %v, got: %v", expectedCiphers, cfg.CipherSuites)
			}

			// With http/2 disabled the negotiated protocols have to be pinned
			// to http/1.1, otherwise the defaults of the server apply.
			if enableHTTP2 {
				if len(cfg.NextProtos) != 0 {
					t.Errorf("expected no protocol restriction, got: %v", cfg.NextProtos)
				}
			} else {
				if !slices.Equal(cfg.NextProtos, []string{"http/1.1"}) {
					t.Errorf("expected the protocols to be pinned to http/1.1, got: %v", cfg.NextProtos)
				}
			}
		})
	}
}

// TestTLSOptionsIndependent verifies that the returned options do not share
// state, as they are handed to both the webhook and the metrics server.
func TestTLSOptionsIndependent(t *testing.T) {
	opts := tlsOptions(false)

	first := applyTLSOptions(opts)
	first.CipherSuites = nil
	first.NextProtos = nil

	second := applyTLSOptions(opts)

	if len(second.CipherSuites) == 0 {
		t.Error("expected the cipher suites to be set on a second configuration")
	}

	if len(second.NextProtos) == 0 {
		t.Error("expected the protocols to be set on a second configuration")
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
