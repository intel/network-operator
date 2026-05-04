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

package controller

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	resource "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkv1alpha1 "github.com/intel/network-operator/api/v1alpha1"
	"github.com/intel/network-operator/config/deployments"
)

type HostNICReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
	ReqName   string
}

const (
	dranetContainer = "dranet"
)

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;create;update;delete;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;create;update;delete;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get
//+kubebuilder:rbac:groups=resource.k8s.io,resources=deviceclasses,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaims,verbs=get
//+kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaims/status,verbs=patch;update
//+kubebuilder:rbac:groups=resource.k8s.io,resources=resourceslices,verbs=list;watch;create;update;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=watch;update

func (r *HostNICReconciler) updateClusterRole(ctx context.Context, cp *networkv1alpha1.NetworkClusterPolicy) error {
	cr := deployments.DranetClusterRole()

	var existingCR rbac.ClusterRole
	if err := r.Get(ctx, client.ObjectKey{Name: cr.Name}, &existingCR); err != nil {
		if client.IgnoreNotFound(err) != nil {
			klog.Error(err, "could not fetch DRANet ClusterRole")
			return err
		}

		if err := ctrl.SetControllerReference(cp, cr, r.Scheme); err != nil {
			klog.Error(err, "unable to set DRANet ClusterRole controller reference")
			return err
		}

		if err := r.Create(ctx, cr); err != nil {
			klog.Error(err, "unable to create DRANet ClusterRole")
			return err
		}

		klog.V(3).Info("Created DRANet ClusterRole")
		return nil
	}

	// preserve all metadata, set rules to expected ones
	crWithMetadata := existingCR.DeepCopy()
	crWithMetadata.Rules = cr.Rules

	if len(cmp.Diff(existingCR, *crWithMetadata, cmpopts.EquateEmpty())) > 0 {
		if err := r.Update(ctx, crWithMetadata); err != nil {
			klog.Error(err, "unable to update DRANet ClusterRole")
			return err
		}

		klog.V(3).Info("Updated DRANet ClusterRole")
	} else {
		klog.V(3).Info("No changes to DRANet ClusterRole")
	}

	return nil
}

func (r *HostNICReconciler) updateServiceAccount(ctx context.Context, cp *networkv1alpha1.NetworkClusterPolicy) error {
	sa := deployments.DranetServiceAccount()
	sa.Namespace = r.Namespace

	var existingSA core.ServiceAccount
	if err := r.Get(ctx, client.ObjectKey{Name: sa.Name, Namespace: sa.Namespace}, &existingSA); err != nil {
		if client.IgnoreNotFound(err) != nil {
			klog.Errorf("could not fetch DRANet ServiceAccount: %v", err)
			return err
		}

		if err := ctrl.SetControllerReference(cp, sa, r.Scheme); err != nil {
			klog.Errorf("unable to set DRANet ServiceAccount controller reference: %v", err)
			return err
		}

		if err := r.Create(ctx, sa); err != nil {
			klog.Errorf("unable to create DRANet ServiceAccount: %v", err)
			return err
		}

		klog.V(3).Info("Created DRANet ServiceAccount")
		return nil
	}

	// Name and Namespace are the only information in a ServiceAccount, and
	// those are already present as the ServiceAccount was found
	klog.V(3).Info("No changes to DRANet ServiceAccount")

	return nil
}

func (r *HostNICReconciler) updateClusterRoleBinding(ctx context.Context, cp *networkv1alpha1.NetworkClusterPolicy) error {
	crb := deployments.DranetClusterRoleBinding()
	for i, s := range crb.Subjects {
		if s.Kind == rbac.ServiceAccountKind {
			crb.Subjects[i].Namespace = r.Namespace
		}
	}

	var existingCRB rbac.ClusterRoleBinding
	if err := r.Get(ctx, client.ObjectKey{Name: crb.Name}, &existingCRB); err != nil {
		if client.IgnoreNotFound(err) != nil {
			klog.Errorf("could not fetch DRANet ClusterRoleBinding: %v", err)
			return err
		}

		if err := ctrl.SetControllerReference(cp, crb, r.Scheme); err != nil {
			klog.Errorf("unable to set DRANet ClusterRoleBinding controller reference: %v", err)
			return err
		}

		if err = r.Create(ctx, crb); err != nil {
			klog.Errorf("unable to create DRANet ClusterRoleBinding: %v", err)
			return err
		}

		klog.V(3).Info("Created DRANet ClusterRoleBinding")
		return nil
	}

	// preserve all metadata, set roleref and subjects to intended ones
	crbWithMetadata := existingCRB.DeepCopy()
	crbWithMetadata.RoleRef = crb.RoleRef
	crbWithMetadata.Subjects = crb.Subjects

	if len(cmp.Diff(existingCRB, *crbWithMetadata, cmpopts.EquateEmpty())) > 0 {
		if err := r.Update(ctx, crbWithMetadata); err != nil {
			klog.Errorf("unable to update DRANet ClusterRoleBinding: %v", err)
			return err
		}

		klog.V(3).Infof("Updated DRANet ClusterRoleBinding")
	} else {
		klog.V(3).Info("No changes to DRANet ClusterRoleBinding")
	}

	return nil
}

func (r *HostNICReconciler) updateDeviceClass(ctx context.Context, cp *networkv1alpha1.NetworkClusterPolicy) error {
	rdc := deployments.DranetRDMADeviceClass()
	if cp.Spec.HostNicScaleOut.Dranet.RDMADeviceClass == nil {
		klog.Infof("No DeviceClass defined, not installing one")
		return nil
	}

	if cp.Spec.HostNicScaleOut.Dranet.RDMADeviceClass.Name != "" {
		rdc.Name = cp.Spec.HostNicScaleOut.Dranet.RDMADeviceClass.Name
	}

	var existingDC resource.DeviceClass
	if err := r.Get(ctx, client.ObjectKey{Name: rdc.Name}, &existingDC); err != nil {
		if client.IgnoreNotFound(err) != nil {
			klog.Errorf("could not fetch DRANet DeviceClass: %v", err)
			return err
		}

		if err := ctrl.SetControllerReference(cp, rdc, r.Scheme); err != nil {
			klog.Errorf("unable to set DRANet DeviceClass controller reference: %v", err)
			return err
		}

		if err := r.Create(ctx, rdc); err != nil {
			klog.Errorf("unable to create DRANet DeviceClass: %v", err)
			return err
		}

		klog.V(3).Info("Created DRANet DeviceClass")
		return nil
	}

	// preserve all metadata, set spec to the intended one
	rdcWithMetadata := existingDC.DeepCopy()
	rdcWithMetadata.Spec = rdc.Spec

	if len(cmp.Diff(existingDC, *rdcWithMetadata, cmpopts.EquateEmpty())) > 0 {
		if err := r.Update(ctx, rdcWithMetadata); err != nil {
			klog.Errorf("unable to update DRANet DeviceClass: %v", err)
			return err
		}

		klog.V(3).Infof("Updated DRANet DeviceClass")
	} else {
		klog.V(3).Info("No changes to DRANet DeviceClass")
	}

	return nil
}

func (r *HostNICReconciler) modifyDranetDaemonSet(ds *apps.DaemonSet, cp *networkv1alpha1.NetworkClusterPolicy) {
	spec := ds.Spec.Template.Spec
	for i, container := range spec.Containers {
		if container.Name != dranetContainer {
			continue
		}

		if cp.Spec.HostNicScaleOut.Dranet.Image != "" {
			spec.Containers[i].Image = cp.Spec.HostNicScaleOut.Dranet.Image
		}

		if cp.Spec.HostNicScaleOut.Dranet.PullPolicy != "" {
			spec.Containers[i].ImagePullPolicy = core.PullPolicy(cp.Spec.HostNicScaleOut.Dranet.PullPolicy)
		}
	}
}

func (r *HostNICReconciler) updateDranetDaemonSet(ctx context.Context, cp *networkv1alpha1.NetworkClusterPolicy) error {
	ds := deployments.DranetDaemonSet()
	ds.Namespace = r.Namespace
	r.modifyDranetDaemonSet(ds, cp)

	var existingDS apps.DaemonSet
	if err := r.Get(ctx, client.ObjectKey{Name: ds.Name, Namespace: ds.Namespace}, &existingDS); err != nil {
		if client.IgnoreNotFound(err) != nil {
			klog.Errorf("could not fetch DRANet DaemonSet: %v", err)
			return err
		}

		if err := ctrl.SetControllerReference(cp, ds, r.Scheme); err != nil {
			klog.Errorf("unable to set DRANet DaemonSet controller reference: %v", err)
			return err
		}

		if err = r.Create(ctx, ds); err != nil {
			klog.Errorf("unable to create DRANet DaemonSet: %v", err)
			return err
		}

		klog.V(3).Info("Created DRANet DaemonSet")
		return nil
	}

	dsUpdated := existingDS.DeepCopy()
	r.modifyDranetDaemonSet(dsUpdated, cp)
	if len(cmp.Diff(existingDS.Spec.Template.Spec, dsUpdated.Spec.Template.Spec, cmpopts.EquateEmpty())) > 0 {
		if err := r.Update(ctx, dsUpdated); err != nil {
			klog.Errorf("unable to update DRANet DaemonSet: %v", err)
			return err
		}

		klog.V(3).Infof("Updated DRANet DaemonSet")
	} else {
		klog.V(3).Info("No changes to DRANet DaemonSet")
	}

	return nil
}

func (r *HostNICReconciler) Reconcile(ctx context.Context, cp *networkv1alpha1.NetworkClusterPolicy) (ctrl.Result, error) {
	if cp == nil || cp.Spec.ConfigurationType != hostNicScaleOutSelection {
		return ctrl.Result{}, nil
	}

	if !cp.Spec.HostNicScaleOut.InstallDRANet {
		klog.V(3).Info("DRANet not to be installed")
		return ctrl.Result{}, nil
	}

	if err := r.updateClusterRole(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.updateClusterRoleBinding(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.updateServiceAccount(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.updateDeviceClass(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.updateDranetDaemonSet(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
