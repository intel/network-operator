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
	"path/filepath"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

// GaudiNICReconciler reconciles a NetworkClusterPolicy object
type GaudiNICReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Namespace   string
	ReqName     string
	isOpenShift bool
}

const (
	ownerKey = ".metadata.controller"

	layerSelectionL2 = "L2"
	layerSelectionL3 = "L3"

	gaudinetPathHost      = "/etc/habanalabs/gaudinet.json"
	gaudinetPathContainer = "/host" + gaudinetPathHost

	discoveryContainer = "configurator"
	lldpadID           = "lldpad"

	emptyDirSize = "32Mi"

	scaleOutMonitoringPort = 50152
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

func delHostVolumeIfExists(ds *apps.DaemonSet, volumeName string) {
	index := -1
	for i, vol := range ds.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName {
			index = i
			break
		}
	}

	if index >= 0 {
		ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes[:index], ds.Spec.Template.Spec.Volumes[index+1:]...)
	}

	for containerIndex := range ds.Spec.Template.Spec.Containers {
		c := &ds.Spec.Template.Spec.Containers[containerIndex]
		index := -1
		for i, volMount := range c.VolumeMounts {
			if volMount.Name == volumeName {
				index = i
				break
			}
		}

		if index >= 0 {
			c.VolumeMounts = append(c.VolumeMounts[:index], c.VolumeMounts[index+1:]...)
			break
		}
	}
}

func (r *GaudiNICReconciler) createOpenShiftCollateral(ctx context.Context, log logr.Logger, parent metav1.Object, serviceAccountName string) {
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

func addLLDPADContainer(ds *apps.DaemonSet, netconf *networkv1alpha1.NetworkClusterPolicy) {
	spec := &ds.Spec.Template.Spec

	volumeFound := false
	for _, v := range spec.Volumes {
		if v.Name == lldpadID {
			volumeFound = true
		}
	}

	if !volumeFound {
		size, _ := resource.ParseQuantity(emptyDirSize)
		spec.Volumes = append(spec.Volumes, v1.Volume{
			Name: lldpadID,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{
					SizeLimit: &size,
				},
			},
		})
	}

	contFound := false
	for _, c := range spec.Containers {
		if c.Name == lldpadID {
			contFound = true
		}
	}

	if !contFound {
		c := discovery.LLDPADContainer()
		c.Image = netconf.Spec.GaudiScaleOut.Image
		c.ImagePullPolicy = v1.PullPolicy(netconf.Spec.GaudiScaleOut.PullPolicy)

		// TODO: add possibility to set cpu&memory requests/limits

		spec.Containers = append(spec.Containers, *c)
	}
}

func removeLLDPAD(ds *apps.DaemonSet) {
	spec := &ds.Spec.Template.Spec

	for idx, c := range spec.Containers {
		if c.Name == lldpadID {
			spec.Containers = append(spec.Containers[:idx], spec.Containers[idx+1:]...)
			break
		}
	}

	for idx, v := range ds.Spec.Template.Spec.Volumes {
		if v.Name == lldpadID {
			spec.Volumes = append(spec.Volumes[:idx], spec.Volumes[idx+1:]...)
		}
	}
}

func addMetricsPort(ds *apps.DaemonSet) {
	spec := &ds.Spec.Template.Spec

	for contIndex := range spec.Containers {
		c := &spec.Containers[contIndex]

		if c.Name == discoveryContainer {
			for _, p := range c.Ports {
				if p.HostPort == scaleOutMonitoringPort {
					return
				}
			}

			c.Ports = append(c.Ports, v1.ContainerPort{
				HostPort:      scaleOutMonitoringPort,
				ContainerPort: scaleOutMonitoringPort,
			})

			break
		}
	}
}

func delMetricsPortIfExists(ds *apps.DaemonSet) {
	spec := &ds.Spec.Template.Spec

	for contIndex := range spec.Containers {
		c := &spec.Containers[contIndex]

		if c.Name == discoveryContainer {
			for i, p := range c.Ports {
				if p.HostPort == scaleOutMonitoringPort {
					c.Ports = append(c.Ports[:i], c.Ports[i+1:]...)
					break
				}
			}
			break
		}
	}
}

func updateGaudiScaleOutDaemonSet(ds *apps.DaemonSet, netconf *networkv1alpha1.NetworkClusterPolicy, namespace string) {
	ds.Name = netconf.Name
	ds.ObjectMeta.Namespace = namespace

	ds.Spec.Template.Spec.Containers[0].ImagePullPolicy = v1.PullPolicy(netconf.Spec.GaudiScaleOut.PullPolicy)

	if len(netconf.Spec.NodeSelector) > 0 {
		ds.Spec.Template.Spec.NodeSelector = netconf.Spec.NodeSelector
	}

	if len(netconf.Spec.GaudiScaleOut.Image) > 0 {
		ds.Spec.Template.Spec.Containers[0].Image = netconf.Spec.GaudiScaleOut.Image
	}

	// TODO: add possibility to set cpu&memory requests/limits

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
	case layerSelectionL2:
		delHostVolumeIfExists(ds, "gaudinetpath")
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

	if netconf.Spec.GaudiScaleOut.NetworkMetrics {
		args = append(args, fmt.Sprintf("--metrics-bind-address=:%d", scaleOutMonitoringPort))
		addMetricsPort(ds)
	} else {
		delMetricsPortIfExists(ds)
	}

	ds.Spec.Template.Spec.Containers[0].Args = args

	if netconf.Spec.GaudiScaleOut.EnableLLDPAD {
		addLLDPADContainer(ds, netconf)
	} else {
		removeLLDPAD(ds)
	}
}

func (r *GaudiNICReconciler) createGaudiScaleOutDaemonset(netconf client.Object, ctx context.Context, log logr.Logger) (ctrl.Result, error) {
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

func (r *GaudiNICReconciler) updateStatus(nc *networkv1alpha1.NetworkClusterPolicy, ds *apps.DaemonSet, ctx context.Context, log logr.Logger) (ctrl.Result, error) {

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

func (r *GaudiNICReconciler) Reconcile(ctx context.Context, clusterPolicy *networkv1alpha1.NetworkClusterPolicy) (ctrl.Result, error) {

	if clusterPolicy == nil || clusterPolicy.Spec.ConfigurationType != gaudiScaleOutSelection {
		return ctrl.Result{}, nil
	}

	log := log.FromContext(ctx)

	// fetch possible existing daemonset

	ds := &apps.DaemonSet{}
	if err := r.Get(ctx, client.ObjectKey{Name: clusterPolicy.Name, Namespace: r.Namespace}, ds); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch DaemonSet")

			return ctrl.Result{}, err
		}
		return r.createGaudiScaleOutDaemonset(clusterPolicy, ctx, log)
	}

	originalDs := ds.DeepCopy()

	updateGaudiScaleOutDaemonSet(ds, clusterPolicy, r.Namespace)

	dsDiff := cmp.Diff(originalDs.Spec.Template.Spec, ds.Spec.Template.Spec, diff.IgnoreUnset())
	if len(dsDiff) > 0 {
		log.Info("DS difference", "diff", dsDiff)

		if err := r.Update(ctx, ds); err != nil {
			log.Error(err, "unable to update daemonset", "DaemonSet", ds)

			return ctrl.Result{}, err
		}
	}

	// Update Pods Statuses

	return r.updateStatus(clusterPolicy, ds, ctx, log)
}
