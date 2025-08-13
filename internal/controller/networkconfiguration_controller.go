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
	"fmt"
	"os"
	"path/filepath"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	networkv1alpha1 "github.com/intel/network-operator/api/v1alpha1"
	discovery "github.com/intel/network-operator/config/discovery"
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
	isOpenShift bool
}

const (
	ownerKey = ".metadata.controller"

	gaudiScaleOutSelection = "gaudi-so"

	layerSelectionL2 = "L2"
	layerSelectionL3 = "L3"

	gaudinetPathHost      = "/etc/habanalabs/gaudinet.json"
	gaudinetPathContainer = "/host" + gaudinetPathHost

	lldpadContainer = "lldpad"
	lldpadVolume    = "lldpad"
)

func addHostVolume(ds *apps.DaemonSet, volumeType v1.HostPathType, volumeName, hostPath, containerPath string) {
	for _, vol := range ds.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName {
			return
		}
	}

	volumeAdd := v1.Volume{
		Name: volumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: hostPath,
				Type: &volumeType,
			},
		},
	}

	mountAdd := v1.VolumeMount{
		Name:      volumeName,
		ReadOnly:  false,
		MountPath: containerPath,
	}

	if len(ds.Spec.Template.Spec.Volumes) == 0 {
		ds.Spec.Template.Spec.Volumes = []v1.Volume{volumeAdd}
	} else {
		ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes, volumeAdd)
	}

	if len(ds.Spec.Template.Spec.Containers) > 0 {
		c := &ds.Spec.Template.Spec.Containers[0]

		if len(c.VolumeMounts) == 0 {
			c.VolumeMounts = []v1.VolumeMount{mountAdd}
		} else {
			c.VolumeMounts = append(c.VolumeMounts, mountAdd)
		}
	}
}

func (r *NetworkClusterPolicyReconciler) createOpenShiftCollateral(ctx context.Context, log logr.Logger, parent metav1.Object, serviceAccountName string) {
	if serviceAccountName == "" {
		return
	}

	log.Info("Creating OpenShift collateral")

	sa := discovery.GaudiLinkDiscoveryServiceAccount()
	sa.Name = serviceAccountName
	sa.ObjectMeta.Namespace = r.Namespace

	if err := ctrl.SetControllerReference(parent, sa, r.Scheme); err != nil {
		log.Error(err, "unable to set controller reference (service account)")

		return
	}

	if err := r.Create(ctx, sa); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			log.Error(err, "unable to create service account")

			return
		}
	}

	log.Info("Service account created", "name", sa.Name)

	rb := discovery.OpenShiftRoleBinding()
	rb.Name = serviceAccountName + "-rb"
	rb.ObjectMeta.Namespace = r.Namespace
	rb.Subjects = []rbac.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      serviceAccountName,
			Namespace: r.Namespace,
		},
	}

	if err := ctrl.SetControllerReference(parent, rb, r.Scheme); err != nil {
		log.Error(err, "unable to set controller reference (rolebinding)")

		return
	}

	if err := r.Create(ctx, rb); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			log.Error(err, "unable to create role binding")

			return
		}
	}

	log.Info("Role binding created", "name", rb.Name)
}

func removeLLDPAD(ds *apps.DaemonSet) {
	spec := &ds.Spec.Template.Spec

	for idx, c := range spec.Containers {
		if c.Name == lldpadContainer {
			spec.Containers = append(spec.Containers[:idx], spec.Containers[idx+1:]...)
			break
		}
	}

	for idx, v := range ds.Spec.Template.Spec.Volumes {
		if v.Name == lldpadVolume {
			spec.Volumes = append(spec.Volumes[:idx], spec.Volumes[idx+1:]...)
		}
	}
}

func updateGaudiScaleOutDaemonSet(ds *apps.DaemonSet, netconf *networkv1alpha1.NetworkClusterPolicy, namespace string) {
	ds.Name = netconf.Name
	ds.ObjectMeta.Namespace = namespace

	if len(netconf.Spec.NodeSelector) > 0 {
		ds.Spec.Template.Spec.NodeSelector = netconf.Spec.NodeSelector
	}

	if len(netconf.Spec.GaudiScaleOut.Image) > 0 {
		ds.Spec.Template.Spec.Containers[0].Image = netconf.Spec.GaudiScaleOut.Image
	}

	args := []string{
		"--configure=true", "--keep-running",
		fmt.Sprintf("--mode=%s", netconf.Spec.GaudiScaleOut.Layer),
	}

	// Add log level to the args
	if netconf.Spec.LogLevel > 0 {
		args = append(args, fmt.Sprintf("--v=%d", netconf.Spec.LogLevel))
	}

	if netconf.Spec.GaudiScaleOut.MTU > 0 {
		args = append(args, fmt.Sprintf("--mtu=%d", netconf.Spec.GaudiScaleOut.MTU))
	}

	if netconf.Spec.GaudiScaleOut.DisableNetworkManager {
		args = append(args, "--disable-networkmanager")
		addHostVolume(ds, v1.HostPathDirectoryOrCreate, "var-run-dbus", "/var/run/dbus", "/var/run/dbus")
		addHostVolume(ds, v1.HostPathDirectoryOrCreate, "networkmanager", "/etc/NetworkManager", "/etc/NetworkManager")
	}

	switch netconf.Spec.GaudiScaleOut.Layer {
	case layerSelectionL3:
		args = append(args, "--wait=90s", fmt.Sprintf("--gaudinet=%s", gaudinetPathContainer))

		addHostVolume(ds, v1.HostPathDirectoryOrCreate, "gaudinetpath", filepath.Dir(gaudinetPathHost), filepath.Dir(gaudinetPathContainer))
	}

	if netconf.Spec.GaudiScaleOut.PFCPriorities != "" {
		pfcEnabled := ""
		switch netconf.Spec.GaudiScaleOut.PFCPriorities {
		case "00000000":
			pfcEnabled = "none"
		case "11110000":
			pfcEnabled = "0,1,2,3"
		}
		args = append(args, fmt.Sprintf("--pfc=%s", pfcEnabled))
	}

	ds.Spec.Template.Spec.Containers[0].Args = args

	if !netconf.Spec.GaudiScaleOut.EnableLLDPAD {
		removeLLDPAD(ds)
	}
}

func (r *NetworkClusterPolicyReconciler) createGaudiScaleOutDaemonset(netconf client.Object, ctx context.Context, log logr.Logger) (ctrl.Result, error) {
	ds := discovery.GaudiDiscoveryDaemonSet()

	cr := netconf.(*networkv1alpha1.NetworkClusterPolicy)

	log.Info("Creating Gaudi Scale-Out DaemonSet", "name", cr.Name)

	saName := ""
	if r.isOpenShift {
		saName = cr.Name + "-sa"
	}

	ds.Spec.Template.Spec.ServiceAccountName = saName

	updateGaudiScaleOutDaemonSet(ds, cr, r.Namespace)

	if err := ctrl.SetControllerReference(netconf.(metav1.Object), ds, r.Scheme); err != nil {
		log.Error(err, "unable to set controller reference")

		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, ds); err != nil {
		log.Error(err, "unable to create DaemonSet")

		return ctrl.Result{}, err
	}

	log.Info("Gaudi scale-out daemonset created")

	if saName != "" {
		r.createOpenShiftCollateral(ctx, log, netconf.(metav1.Object), saName)
	}

	return ctrl.Result{}, nil
}

func (r *NetworkClusterPolicyReconciler) createDaemonSet(ctx context.Context, netconf client.Object, log logr.Logger) (ctrl.Result, error) {
	cr := netconf.(*networkv1alpha1.NetworkClusterPolicy)

	switch cr.Spec.ConfigurationType {
	case gaudiScaleOutSelection:
		return r.createGaudiScaleOutDaemonset(netconf, ctx, log)
	default:
		log.Info("Unknown configuration type, this shouldn't happen!", "type", cr.Spec.ConfigurationType)

		return ctrl.Result{}, os.ErrInvalid
	}
}

func (r *NetworkClusterPolicyReconciler) updateDaemonSet(ds *apps.DaemonSet, netconf client.Object) {
	cr := netconf.(*networkv1alpha1.NetworkClusterPolicy)

	switch cr.Spec.ConfigurationType {
	case gaudiScaleOutSelection:
		updateGaudiScaleOutDaemonSet(ds, cr, r.Namespace)
	default:
		panic("Unknown configuration type, this shouldn't happen!")
	}
}

func (r *NetworkClusterPolicyReconciler) updateStatus(rawObj client.Object, ds *apps.DaemonSet, ctx context.Context, log logr.Logger) (ctrl.Result, error) {
	nc := rawObj.(*networkv1alpha1.NetworkClusterPolicy)

	updated := false

	if nc.Status.Targets != ds.Status.DesiredNumberScheduled {
		nc.Status.Targets = ds.Status.DesiredNumberScheduled
		updated = true
	}

	if nc.Status.ReadyNodes != ds.Status.NumberReady {
		nc.Status.ReadyNodes = ds.Status.NumberReady
		updated = true
	}

	nc.Status.Errors = []string{}

	// Update status if there's no State yet.
	if len(nc.Status.State) == 0 {
		updated = true
	}

	if nc.Status.Targets == 0 {
		nc.Status.State = "No targets"
	} else if nc.Status.ReadyNodes < nc.Status.Targets {
		nc.Status.State = "Working on it.."
	} else {
		nc.Status.State = "All good"
	}

	if updated {
		if err := r.Status().Update(ctx, nc); apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "unable to update network conf status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func createEmptyObject() client.Object {
	return &networkv1alpha1.NetworkClusterPolicy{}
}

func (r *NetworkClusterPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconcile now.")

	netConfObj := createEmptyObject()

	if err := r.Get(ctx, req.NamespacedName, netConfObj); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch NetworkClusterPolicies")
		}

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// fetch possible existing daemonset

	var olderDs apps.DaemonSetList
	if err := r.List(ctx, &olderDs, client.InNamespace(r.Namespace), client.MatchingFields{ownerKey: req.Name}); err != nil {
		log.Error(err, "unable to list child DaemonSets")

		return ctrl.Result{}, err
	}

	if len(olderDs.Items) == 0 {
		return r.createDaemonSet(ctx, netConfObj, log)
	}

	// Update DaemonSet

	ds := &olderDs.Items[0]
	originalDs := ds.DeepCopy()

	r.updateDaemonSet(ds, netConfObj)

	dsDiff := cmp.Diff(originalDs.Spec.Template.Spec, ds.Spec.Template.Spec, diff.IgnoreUnset())
	if len(dsDiff) > 0 {
		log.Info("DS difference", "diff", dsDiff)

		if err := r.Update(ctx, ds); err != nil {
			log.Error(err, "unable to update daemonset", "DaemonSet", ds)

			return ctrl.Result{}, err
		}
	}

	// Update Pods Statuses

	return r.updateStatus(netConfObj, ds, ctx, log)
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
