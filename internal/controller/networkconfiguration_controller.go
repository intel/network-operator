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

package controller

import (
	"context"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkv1alpha1 "github.com/intel/network-operator/api/v1alpha1"
)

//+kubebuilder:rbac:groups=intel.com,resources=networkclusterpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=intel.com,resources=networkclusterpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=intel.com,resources=networkclusterpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;create;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;create;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch

// NetworkClusterPolicyReconciler reconciles a NetworkClusterPolicy object
type NetworkClusterPolicyReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Namespace   string
	ReqName     string
	isOpenShift bool
}

type SubControllerInterface interface {
	Reconcile(ctx context.Context, cp *networkv1alpha1.NetworkClusterPolicy) (ctrl.Result, error)
}

const (
	gaudiScaleOutSelection   = "gaudi-so"
	hostNicScaleOutSelection = "hostnic-so"
)

func createEmptyObject() client.Object {
	return &networkv1alpha1.NetworkClusterPolicy{}
}

func (r *NetworkClusterPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconcile now.")

	r.ReqName = req.Name

	netConfObj := createEmptyObject()

	if err := r.Get(ctx, req.NamespacedName, netConfObj); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch NetworkClusterPolicies")
		}

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cp := netConfObj.(*networkv1alpha1.NetworkClusterPolicy)

	// Handle sub-controllers
	subControllers := []SubControllerInterface{}

	subControllers = append(subControllers, &GaudiNICReconciler{Client: r.Client, Scheme: r.Scheme, Namespace: r.Namespace, ReqName: r.ReqName, isOpenShift: r.isOpenShift})
	subControllers = append(subControllers, &HostNICReconciler{Client: r.Client, Scheme: r.Scheme, Namespace: r.Namespace, ReqName: r.ReqName})

	log.Info("Running subcontrollers")
	for _, subController := range subControllers {
		if res, err := subController.Reconcile(ctx, cp); err != nil {
			log.Error(err, "Sub-controller returned error")
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

func indexDaemonSets(ctx context.Context, mgr ctrl.Manager, apiGVString, pluginKind string) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &apps.DaemonSet{}, ownerKey,
		func(rawObj client.Object) []string {
			// grab the DaemonSet object, extract the owner...
			ds := rawObj.(*apps.DaemonSet)
			owner := metav1.GetControllerOf(ds)

			if owner == nil {
				return nil
			}

			// make sure it's a network configuration
			if owner.APIVersion != apiGVString || owner.Kind != pluginKind {
				return nil
			}

			// and if so, return it.
			return []string{owner.Name}
		})
}

func indexPods(ctx context.Context, mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &v1.Pod{}, ownerKey,
		func(rawObj client.Object) []string {
			// grab the Pod object, extract the owner...
			pod := rawObj.(*v1.Pod)
			owner := metav1.GetControllerOf(pod)

			if owner == nil {
				return nil
			}

			// make sure it's a DaemonSet
			if owner.APIVersion != apps.SchemeGroupVersion.String() || owner.Kind != "DaemonSet" {
				return nil
			}

			// and if so, return it.
			return []string{owner.Name}
		})
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkClusterPolicyReconciler) SetupWithManager(mgr ctrl.Manager, isOpenShift bool) error {
	r.Scheme = mgr.GetScheme()
	r.isOpenShift = isOpenShift

	ctx := context.Background()
	apiGVString := networkv1alpha1.GroupVersion.String()
	kind := "NetworkClusterPolicy"

	// Index DaemonSets (CR).
	if err := indexDaemonSets(ctx, mgr, apiGVString, kind); err != nil {
		return err
	}

	// Index Pods with their owner (DaemonSet).
	if err := indexPods(ctx, mgr); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1alpha1.NetworkClusterPolicy{}).
		Owns(&apps.DaemonSet{}).
		Complete(r)
}
