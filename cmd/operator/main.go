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

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"slices"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	networkv1alpha1 "github.com/intel/network-operator/api/v1alpha1"
	"github.com/intel/network-operator/internal/controller"
	buildVersion "github.com/intel/network-operator/internal/version"

	//+kubebuilder:scaffold:imports

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

var (
	scheme          = runtime.NewScheme()
	setupLog        = ctrl.Log.WithName("setup")
	openShiftGroups = []string{"route.openshift.io", "security.openshift.io"}
)

const (
	defaultOperatorNamespace = "intel-network-operator"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(networkv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// options holds the command line configuration of the operator.
type options struct {
	metricsAddr          string
	probeAddr            string
	enableLeaderElection bool
	secureMetrics        bool
	enableHTTP2          bool
	zapOptions           zap.Options
}

// parseFlags registers the operator command line flags on the given flag set
// and parses args with it.
func parseFlags(fs *flag.FlagSet, args []string) (options, error) {
	opts := options{
		zapOptions: zap.Options{
			Development: true,
		},
	}

	fs.StringVar(&opts.metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	fs.StringVar(&opts.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&opts.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&opts.secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	fs.BoolVar(&opts.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	opts.zapOptions.BindFlags(fs)

	klog.InitFlags(fs)

	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	return opts, nil
}

// tlsOptions returns the TLS configuration applied to both the metrics and the
// webhook server.
func tlsOptions(enableHTTP2 bool) []func(*tls.Config) {
	tlsOpts := []func(*tls.Config){
		func(cfg *tls.Config) {
			cfg.MinVersion = tls.VersionTLS12
			cfg.MaxVersion = tls.VersionTLS12
			cfg.CipherSuites = []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			}
		},
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			setupLog.Info("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		})
	}

	return tlsOpts
}

// metricsOptions returns the configuration of the metrics server. The metrics
// endpoint is enabled in 'config/default/kustomization.yaml'.
// More info:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/metrics/server
// - https://book.kubebuilder.io/reference/metrics.html
func metricsOptions(addr string, secure bool, tlsOpts []func(*tls.Config)) metricsserver.Options {
	metricsServerOptions := metricsserver.Options{
		BindAddress:   addr,
		SecureServing: secure,
		TLSOpts:       tlsOpts,
	}

	if secure {
		// More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	return metricsServerOptions
}

// operatorNamespace returns the namespace the operator deploys its workloads
// into.
func operatorNamespace() string {
	ns := os.Getenv("OPERATOR_NAMESPACE")
	if ns == "" {
		ns = defaultOperatorNamespace
	}

	return ns
}

// webhooksEnabled reports whether the admission webhooks are to be registered.
func webhooksEnabled() bool {
	return os.Getenv("ENABLE_WEBHOOKS") != "false"
}

func isOpenShift() (bool, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return false, err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return false, err
	}

	apiGroups, err := discoveryClient.ServerGroups()
	if err != nil {
		return false, err
	}

	for _, group := range apiGroups.Groups {
		if slices.Contains(openShiftGroups, group.Name) {
			return true, nil
		}
	}

	return false, nil
}

func main() {
	opts, err := parseFlags(flag.CommandLine, os.Args[1:])
	if err != nil {
		setupLog.Error(err, "unable to parse command line flags")
		os.Exit(1)
	}

	buildVersion.PrintBuildDetails()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts.zapOptions)))

	tlsOpts := tlsOptions(opts.enableHTTP2)

	ns := operatorNamespace()

	setupLog.Info("Using namespace:", "ns", ns)

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOptions(opts.metricsAddr, opts.secureMetrics, tlsOpts),
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: opts.probeAddr,
		LeaderElection:         opts.enableLeaderElection,
		LeaderElectionID:       "9a8a7ba6.intel.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	isInOpenShift, err := isOpenShift()
	if err != nil {
		setupLog.Error(err, "unable to check if running in OpenShift")
		os.Exit(1)
	}

	if isInOpenShift {
		setupLog.Info("Detected OpenShift environment")
	}

	if err = (&controller.NetworkClusterPolicyReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Namespace: ns,
	}).SetupWithManager(mgr, isInOpenShift); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NetworkClusterPolicy")
		os.Exit(1)
	}
	if webhooksEnabled() {
		if err = (&networkv1alpha1.NetworkClusterPolicy{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "NetworkClusterPolicy")
			os.Exit(1)
		}
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
