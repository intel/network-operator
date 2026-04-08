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

package v1alpha1

import (
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var netpolicylog = logf.Log.WithName("nicclusterpolicy-resource")

const (
	gaudiScaleOut   = "gaudi-so"
	hostNicScaleOut = "hostnic-so"
)

type emptyNodeSelectorError struct{}

func (e emptyNodeSelectorError) Error() string {
	return "empty node-selector"
}

type invalidNodeSelector struct{}

func (e invalidNodeSelector) Error() string {
	return "invalid node selector"
}

type unknownConfigurationError struct{}

func (e unknownConfigurationError) Error() string {
	return "unknown error"
}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *NetworkClusterPolicy) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-intel-com-v1alpha1-networkclusterpolicy,mutating=true,failurePolicy=fail,sideEffects=None,groups=intel.com,resources=networkclusterpolicy,verbs=create;update,versions=v1alpha1,name=mnetworkclusterpolicy.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &NetworkClusterPolicy{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *NetworkClusterPolicy) Default() {
	netpolicylog.Info("default", "name", r.Name)

	switch r.Spec.ConfigurationType {
	case gaudiScaleOut:
		if len(r.Spec.GaudiScaleOut.Image) == 0 {
			r.Spec.GaudiScaleOut.Image = "intel/intel-network-linkdiscovery:latest"
		}
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
//+kubebuilder:webhook:path=/validate-intel-com-v1alpha1-networkclusterpolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=intel.com,resources=networkclusterpolicy,verbs=create;update,versions=v1alpha1,name=vnetworkclusterpolicy.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &NetworkClusterPolicy{}

var labelHostRegex = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_\.]*)?[A-Za-z0-9]$`)
var labelPathRegex = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9-\._\/]*)?[A-Za-z0-9]$`)
var labelValueRegex = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)

func validateGaudiSoSpec(s GaudiScaleOutSpec) error {
	return nil
}

func validateNodeSelector(nodeSelector map[string]string) error {
	if len(nodeSelector) == 0 {
		return emptyNodeSelectorError{}
	}

	for k, v := range nodeSelector {
		if len(k) > 253 || len(v) > 63 {
			return invalidNodeSelector{}
		}

		if !labelValueRegex.MatchString(v) {
			return invalidNodeSelector{}
		}

		parts := strings.SplitN(k, "/", 2)
		if len(parts) == 1 && !labelHostRegex.MatchString(parts[0]) {
			return invalidNodeSelector{}
		} else if len(parts) == 2 {
			if !labelHostRegex.MatchString(parts[0]) {
				return invalidNodeSelector{}
			}
			if !labelPathRegex.MatchString(parts[1]) {
				return invalidNodeSelector{}
			}
		}
	}

	return nil
}

func validateSpec(s NetworkClusterPolicySpec) (admission.Warnings, error) {
	switch s.ConfigurationType {
	case gaudiScaleOut:
		if err := validateNodeSelector(s.NodeSelector); err != nil {
			return nil, err
		}
		return nil, validateGaudiSoSpec(s.GaudiScaleOut)
	case hostNicScaleOut:
		return nil, nil
	default:
		return nil, unknownConfigurationError{}
	}
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *NetworkClusterPolicy) ValidateCreate() (admission.Warnings, error) {
	netpolicylog.Info("validate create", "name", r.Name)

	return validateSpec(r.Spec)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *NetworkClusterPolicy) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	netpolicylog.Info("validate update", "name", r.Name)

	return validateSpec(r.Spec)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *NetworkClusterPolicy) ValidateDelete() (admission.Warnings, error) {
	netpolicylog.Info("validate delete", "name", r.Name)

	return nil, nil
}
