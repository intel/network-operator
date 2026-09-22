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
	"errors"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	networkv1alpha1 "github.com/intel/network-operator/api/v1alpha1"
	discovery "github.com/intel/network-operator/config/discovery"
)

const testGaudiName = "test-gaudi-policy"

// errGaudiInjected is the failure the interceptors below report back.
var errGaudiInjected = errors.New("injected gaudi failure")

var _ = Describe("GaudiNIC Controller", func() {

	Context("Verify host volume handling", func() {

		// bareDaemonSet returns a DaemonSet whose single container has neither
		// volumes nor volume mounts yet.
		bareDaemonSet := func() *apps.DaemonSet {
			return &apps.DaemonSet{
				Spec: apps.DaemonSetSpec{
					Template: core.PodTemplateSpec{
						Spec: core.PodSpec{
							Containers: []core.Container{{Name: discoveryContainer}},
						},
					},
				},
			}
		}

		It("Should add the first volume and mount", func() {
			ds := bareDaemonSet()

			addHostVolume(ds, core.HostPathDirectoryOrCreate, "var-run-dbus", "/var/run/dbus", "/host/var/run/dbus")

			Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Volumes[0].Name).To(Equal("var-run-dbus"))
			Expect(ds.Spec.Template.Spec.Volumes[0].HostPath).NotTo(BeNil())
			Expect(ds.Spec.Template.Spec.Volumes[0].HostPath.Path).To(Equal("/var/run/dbus"))
			Expect(*ds.Spec.Template.Spec.Volumes[0].HostPath.Type).To(Equal(core.HostPathDirectoryOrCreate))

			mounts := ds.Spec.Template.Spec.Containers[0].VolumeMounts
			Expect(mounts).To(HaveLen(1))
			Expect(mounts[0].Name).To(Equal("var-run-dbus"))
			Expect(mounts[0].MountPath).To(Equal("/host/var/run/dbus"))
			Expect(mounts[0].ReadOnly).To(BeFalse())
		})

		It("Should append a further volume and mount", func() {
			ds := bareDaemonSet()

			addHostVolume(ds, core.HostPathDirectoryOrCreate, "first", "/first", "/host/first")
			addHostVolume(ds, core.HostPathDirectory, "second", "/second", "/host/second")

			Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
			Expect(ds.Spec.Template.Spec.Volumes[1].Name).To(Equal("second"))

			mounts := ds.Spec.Template.Spec.Containers[0].VolumeMounts
			Expect(mounts).To(HaveLen(2))
			Expect(mounts[1].Name).To(Equal("second"))
		})

		It("Should not add the same volume twice", func() {
			ds := bareDaemonSet()

			addHostVolume(ds, core.HostPathDirectoryOrCreate, "once", "/once", "/host/once")
			addHostVolume(ds, core.HostPathDirectoryOrCreate, "once", "/elsewhere", "/host/elsewhere")

			Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Volumes[0].HostPath.Path).To(Equal("/once"))
			Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
		})

		It("Should add a volume without a mount when there is no container", func() {
			ds := bareDaemonSet()
			ds.Spec.Template.Spec.Containers = nil

			addHostVolume(ds, core.HostPathDirectoryOrCreate, "orphan", "/orphan", "/host/orphan")

			Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Containers).To(BeEmpty())
		})

		It("Should remove a volume and its mount", func() {
			ds := bareDaemonSet()

			addHostVolume(ds, core.HostPathDirectoryOrCreate, "first", "/first", "/host/first")
			addHostVolume(ds, core.HostPathDirectoryOrCreate, "second", "/second", "/host/second")

			delHostVolumeIfExists(ds, "first")

			Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Volumes[0].Name).To(Equal("second"))

			mounts := ds.Spec.Template.Spec.Containers[0].VolumeMounts
			Expect(mounts).To(HaveLen(1))
			Expect(mounts[0].Name).To(Equal("second"))
		})

		It("Should ignore the removal of an absent volume", func() {
			ds := bareDaemonSet()
			addHostVolume(ds, core.HostPathDirectoryOrCreate, "kept", "/kept", "/host/kept")

			before := ds.DeepCopy()
			delHostVolumeIfExists(ds, "never-added")

			Expect(cmp.Diff(before.Spec, ds.Spec, cmpopts.EquateEmpty())).To(Equal(""))
		})
	})

	Context("Verify the scale-out DaemonSet arguments", func() {

		It("Should pass the log level on to the configurator", func() {
			ds := discovery.GaudiDiscoveryDaemonSet()
			cp := gaudiPolicy()
			cp.Spec.LogLevel = 4

			updateGaudiScaleOutDaemonSet(ds, cp, testNamespace)

			Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--v=4"))
		})

		It("Should not pass a log level of zero", func() {
			ds := discovery.GaudiDiscoveryDaemonSet()
			cp := gaudiPolicy()

			updateGaudiScaleOutDaemonSet(ds, cp, testNamespace)

			Expect(ds.Spec.Template.Spec.Containers[0].Args).NotTo(ContainElement(HavePrefix("--v=")))
		})

		It("Should name the DaemonSet after the cluster policy", func() {
			ds := discovery.GaudiDiscoveryDaemonSet()
			cp := gaudiPolicy()

			updateGaudiScaleOutDaemonSet(ds, cp, testNamespace)

			Expect(ds.Name).To(Equal(testGaudiName))
			Expect(ds.Namespace).To(Equal(testNamespace))
			Expect(ds.Spec.Template.Spec.NodeSelector).To(Equal(cp.Spec.NodeSelector))
		})

		It("Should keep the shipped node selector when the policy has none", func() {
			ds := discovery.GaudiDiscoveryDaemonSet()
			shipped := ds.Spec.Template.Spec.NodeSelector

			cp := gaudiPolicy()
			cp.Spec.NodeSelector = nil

			updateGaudiScaleOutDaemonSet(ds, cp, testNamespace)

			Expect(ds.Spec.Template.Spec.NodeSelector).To(Equal(shipped))
		})
	})

	Context("Verify the OpenShift collateral", func() {
		var (
			r  *GaudiNICReconciler
			cp *networkv1alpha1.NetworkClusterPolicy
		)

		serviceAccountKey := client.ObjectKey{Name: testGaudiName + "-sa", Namespace: testNamespace}
		roleBindingKey := client.ObjectKey{Name: testGaudiName + "-sa-rb", Namespace: testNamespace}

		BeforeEach(func() {
			cp = gaudiPolicy()
			r = newGaudiNICReconciler(cp)
		})

		It("Should not create anything without a service account name", func() {
			r.createOpenShiftCollateral(ctx, logr.Discard(), cp, "")

			Expect(r.Get(ctx, serviceAccountKey, &core.ServiceAccount{})).To(HaveOccurred())
			Expect(r.Get(ctx, roleBindingKey, &rbac.RoleBinding{})).To(HaveOccurred())
		})

		It("Should create the service account and the role binding", func() {
			r.createOpenShiftCollateral(ctx, logr.Discard(), cp, serviceAccountKey.Name)

			sa := core.ServiceAccount{}
			Expect(r.Get(ctx, serviceAccountKey, &sa)).To(Succeed())
			Expect(sa.OwnerReferences).To(HaveLen(1))
			Expect(sa.OwnerReferences[0].Name).To(Equal(testGaudiName))

			rb := rbac.RoleBinding{}
			Expect(r.Get(ctx, roleBindingKey, &rb)).To(Succeed())
			Expect(rb.OwnerReferences).To(HaveLen(1))
			Expect(rb.OwnerReferences[0].Name).To(Equal(testGaudiName))
			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(rb.Subjects[0].Name).To(Equal(serviceAccountKey.Name))
			Expect(rb.Subjects[0].Namespace).To(Equal(testNamespace))
		})

		It("Should tolerate collateral that already exists", func() {
			r.createOpenShiftCollateral(ctx, logr.Discard(), cp, serviceAccountKey.Name)

			// The second round finds both objects in place and must not treat
			// that as a failure.
			r.createOpenShiftCollateral(ctx, logr.Discard(), cp, serviceAccountKey.Name)

			Expect(r.Get(ctx, serviceAccountKey, &core.ServiceAccount{})).To(Succeed())
			Expect(r.Get(ctx, roleBindingKey, &rbac.RoleBinding{})).To(Succeed())
		})

		It("Should stop when the service account cannot be created", func() {
			r = newGaudiNICReconcilerWithFuncs(interceptor.Funcs{
				Create: failCreateOf(&core.ServiceAccount{}),
			}, cp)

			r.createOpenShiftCollateral(ctx, logr.Discard(), cp, serviceAccountKey.Name)

			Expect(r.Get(ctx, serviceAccountKey, &core.ServiceAccount{})).To(HaveOccurred())

			// The role binding is not attempted once the service account failed.
			Expect(r.Get(ctx, roleBindingKey, &rbac.RoleBinding{})).To(HaveOccurred())
		})

		It("Should stop when the role binding cannot be created", func() {
			r = newGaudiNICReconcilerWithFuncs(interceptor.Funcs{
				Create: failCreateOf(&rbac.RoleBinding{}),
			}, cp)

			r.createOpenShiftCollateral(ctx, logr.Discard(), cp, serviceAccountKey.Name)

			Expect(r.Get(ctx, serviceAccountKey, &core.ServiceAccount{})).To(Succeed())
			Expect(r.Get(ctx, roleBindingKey, &rbac.RoleBinding{})).To(HaveOccurred())
		})

		It("Should not create collateral it cannot own", func() {
			r.Scheme = schemeWithoutClusterPolicy()

			r.createOpenShiftCollateral(ctx, logr.Discard(), cp, serviceAccountKey.Name)

			Expect(r.Get(ctx, serviceAccountKey, &core.ServiceAccount{})).To(HaveOccurred())
			Expect(r.Get(ctx, roleBindingKey, &rbac.RoleBinding{})).To(HaveOccurred())
		})
	})

	Context("Verify the cluster policy status", func() {
		var (
			r  *GaudiNICReconciler
			cp *networkv1alpha1.NetworkClusterPolicy
		)

		BeforeEach(func() {
			cp = gaudiPolicy()
			r = newGaudiNICReconciler(cp)
		})

		// daemonSetWith returns a DaemonSet reporting the given number of
		// desired and ready nodes.
		daemonSetWith := func(desired, ready int32) *apps.DaemonSet {
			return &apps.DaemonSet{
				Status: apps.DaemonSetStatus{
					DesiredNumberScheduled: desired,
					NumberReady:            ready,
				},
			}
		}

		DescribeTable("Derive the state from the DaemonSet status",
			func(desired, ready int32, expectedState string) {
				Expect(r.updateStatus(cp, daemonSetWith(desired, ready), ctx, logr.Discard())).To(Equal(ctrl.Result{}))

				stored := networkv1alpha1.NetworkClusterPolicy{}
				Expect(r.Get(ctx, client.ObjectKeyFromObject(cp), &stored)).To(Succeed())
				Expect(stored.Status.Targets).To(Equal(desired))
				Expect(stored.Status.ReadyNodes).To(Equal(ready))
				Expect(stored.Status.State).To(Equal(expectedState))
				Expect(stored.Status.Errors).To(BeEmpty())
			},
			Entry("without any target node", int32(0), int32(0), "No targets"),
			Entry("while nodes are still coming up", int32(3), int32(1), "Working on it.."),
			Entry("once every node is ready", int32(3), int32(3), "All good"),
			Entry("with more ready nodes than desired", int32(2), int32(3), "All good"),
		)

		It("Should not write an unchanged status", func() {
			Expect(r.updateStatus(cp, daemonSetWith(2, 2), ctx, logr.Discard())).To(Equal(ctrl.Result{}))

			before := networkv1alpha1.NetworkClusterPolicy{}
			Expect(r.Get(ctx, client.ObjectKeyFromObject(cp), &before)).To(Succeed())

			Expect(r.updateStatus(cp, daemonSetWith(2, 2), ctx, logr.Discard())).To(Equal(ctrl.Result{}))

			// An unnecessary update would show up as a changed resource version.
			after := networkv1alpha1.NetworkClusterPolicy{}
			Expect(r.Get(ctx, client.ObjectKeyFromObject(cp), &after)).To(Succeed())
			Expect(after.ResourceVersion).To(Equal(before.ResourceVersion))
		})

		It("Should requeue a conflicting status update", func() {
			r = newGaudiNICReconcilerWithFuncs(interceptor.Funcs{
				SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
					return apierrors.NewConflict(
						schema.GroupResource{Group: networkv1alpha1.GroupVersion.Group, Resource: "networkclusterpolicies"},
						testGaudiName, errGaudiInjected)
				},
			}, cp)

			Expect(r.updateStatus(cp, daemonSetWith(1, 1), ctx, logr.Discard())).To(Equal(ctrl.Result{Requeue: true}))
		})

		It("Should report any other status update failure", func() {
			r = newGaudiNICReconcilerWithFuncs(interceptor.Funcs{
				SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
					return errGaudiInjected
				},
			}, cp)

			_, err := r.updateStatus(cp, daemonSetWith(1, 1), ctx, logr.Discard())
			Expect(err).To(MatchError(errGaudiInjected))
		})
	})

	Context("Verify gaudiScaleOut reconciliation", func() {
		var cp *networkv1alpha1.NetworkClusterPolicy

		daemonSetKey := client.ObjectKey{Name: testGaudiName, Namespace: testNamespace}

		BeforeEach(func() {
			cp = gaudiPolicy()
		})

		It("Should ignore a missing cluster policy", func() {
			r := newGaudiNICReconciler()

			Expect(r.Reconcile(ctx, nil)).To(Equal(ctrl.Result{}))
			Expect(r.Get(ctx, daemonSetKey, &apps.DaemonSet{})).To(HaveOccurred())
		})

		It("Should ignore another configuration type", func() {
			cp.Spec.ConfigurationType = hostNicScaleOutSelection
			r := newGaudiNICReconciler(cp)

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))
			Expect(r.Get(ctx, daemonSetKey, &apps.DaemonSet{})).To(HaveOccurred())
		})

		It("Should create the DaemonSet when it is missing", func() {
			r := newGaudiNICReconciler(cp)

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))

			ds := apps.DaemonSet{}
			Expect(r.Get(ctx, daemonSetKey, &ds)).To(Succeed())
			Expect(ds.Spec.Template.Spec.Containers[0].Args).To(
				ContainElements("--configure=true", "--keep-running", "--mode="+layerSelectionL3))
			Expect(ds.OwnerReferences).To(HaveLen(1))
			Expect(ds.OwnerReferences[0].Name).To(Equal(testGaudiName))
			Expect(ds.OwnerReferences[0].Kind).To(Equal("NetworkClusterPolicy"))

			// Outside OpenShift no service account is used, so none is created.
			Expect(ds.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())
			Expect(r.Get(ctx, client.ObjectKey{Name: testGaudiName + "-sa", Namespace: testNamespace},
				&core.ServiceAccount{})).To(HaveOccurred())
		})

		It("Should create the collateral on OpenShift", func() {
			r := newGaudiNICReconciler(cp)
			r.isOpenShift = true

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))

			ds := apps.DaemonSet{}
			Expect(r.Get(ctx, daemonSetKey, &ds)).To(Succeed())
			Expect(ds.Spec.Template.Spec.ServiceAccountName).To(Equal(testGaudiName + "-sa"))

			Expect(r.Get(ctx, client.ObjectKey{Name: testGaudiName + "-sa", Namespace: testNamespace},
				&core.ServiceAccount{})).To(Succeed())
			Expect(r.Get(ctx, client.ObjectKey{Name: testGaudiName + "-sa-rb", Namespace: testNamespace},
				&rbac.RoleBinding{})).To(Succeed())
		})

		It("Should not create a DaemonSet it cannot own", func() {
			r := newGaudiNICReconciler(cp)
			r.Scheme = schemeWithoutClusterPolicy()

			_, err := r.Reconcile(ctx, cp)
			Expect(err).To(HaveOccurred())
			Expect(r.Get(ctx, daemonSetKey, &apps.DaemonSet{})).To(HaveOccurred())
		})

		It("Should report a failing DaemonSet creation", func() {
			r := newGaudiNICReconcilerWithFuncs(interceptor.Funcs{
				Create: failCreateOf(&apps.DaemonSet{}),
			}, cp)

			_, err := r.Reconcile(ctx, cp)
			Expect(err).To(MatchError(errGaudiInjected))
		})

		It("Should report a failing DaemonSet fetch", func() {
			r := newGaudiNICReconcilerWithFuncs(interceptor.Funcs{
				Get: failGetOf(&apps.DaemonSet{}),
			}, cp)

			_, err := r.Reconcile(ctx, cp)
			Expect(err).To(MatchError(errGaudiInjected))
			Expect(r.Get(ctx, daemonSetKey, &apps.DaemonSet{})).To(MatchError(errGaudiInjected))
		})

		It("Should restore a drifted DaemonSet", func() {
			r := newGaudiNICReconciler(cp)

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))

			installed := apps.DaemonSet{}
			Expect(r.Get(ctx, daemonSetKey, &installed)).To(Succeed())

			drifted := installed.DeepCopy()
			drifted.Spec.Template.Spec.Containers[0].Args = nil
			Expect(r.Update(ctx, drifted)).To(Succeed())

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))

			restored := apps.DaemonSet{}
			Expect(r.Get(ctx, daemonSetKey, &restored)).To(Succeed())
			Expect(cmp.Diff(installed.Spec.Template.Spec, restored.Spec.Template.Spec, cmpopts.EquateEmpty())).To(Equal(""))
		})

		It("Should not touch an up-to-date DaemonSet", func() {
			r := newGaudiNICReconciler(cp)

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))

			before := apps.DaemonSet{}
			Expect(r.Get(ctx, daemonSetKey, &before)).To(Succeed())

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))

			// An unnecessary update would show up as a changed resource version.
			after := apps.DaemonSet{}
			Expect(r.Get(ctx, daemonSetKey, &after)).To(Succeed())
			Expect(after.ResourceVersion).To(Equal(before.ResourceVersion))
		})

		It("Should report a failing DaemonSet update", func() {
			// The shipped DaemonSet carries no arguments, so reconciling it
			// against the cluster policy needs an update.
			seeded := discovery.GaudiDiscoveryDaemonSet()
			seeded.Name = testGaudiName
			seeded.Namespace = testNamespace

			r := newGaudiNICReconcilerWithFuncs(interceptor.Funcs{
				Update: failUpdateOf(&apps.DaemonSet{}),
			}, cp, seeded)

			_, err := r.Reconcile(ctx, cp)
			Expect(err).To(MatchError(errGaudiInjected))
		})

		It("Should update the status of an existing DaemonSet", func() {
			seeded := discovery.GaudiDiscoveryDaemonSet()
			seeded.Name = testGaudiName
			seeded.Namespace = testNamespace
			seeded.Status = apps.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 1}

			r := newGaudiNICReconciler(cp, seeded)

			Expect(r.Reconcile(ctx, cp)).To(Equal(ctrl.Result{}))

			stored := networkv1alpha1.NetworkClusterPolicy{}
			Expect(r.Get(ctx, client.ObjectKeyFromObject(cp), &stored)).To(Succeed())
			Expect(stored.Status.Targets).To(Equal(int32(2)))
			Expect(stored.Status.ReadyNodes).To(Equal(int32(1)))
			Expect(stored.Status.State).To(Equal("Working on it.."))
		})
	})
})

// newGaudiNICReconciler returns a Gaudi NIC reconciler backed by an in-memory
// client holding the given objects.
func newGaudiNICReconciler(objs ...client.Object) *GaudiNICReconciler {
	return newGaudiNICReconcilerWithFuncs(interceptor.Funcs{}, objs...)
}

// newGaudiNICReconcilerWithFuncs returns a Gaudi NIC reconciler whose client
// calls the given interceptor functions in place of its own, so that API
// failures can be injected.
func newGaudiNICReconcilerWithFuncs(funcs interceptor.Funcs, objs ...client.Object) *GaudiNICReconciler {
	scheme := gaudiScheme()

	return &GaudiNICReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			WithStatusSubresource(&networkv1alpha1.NetworkClusterPolicy{}).
			WithInterceptorFuncs(funcs).
			Build(),
		Scheme:    scheme,
		Namespace: testNamespace,
		ReqName:   testGaudiName,
	}
}

// gaudiScheme returns a scheme holding every type the Gaudi reconciler touches.
func gaudiScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(core.AddToScheme(scheme)).To(Succeed())
	Expect(rbac.AddToScheme(scheme)).To(Succeed())
	Expect(apps.AddToScheme(scheme)).To(Succeed())
	Expect(networkv1alpha1.AddToScheme(scheme)).To(Succeed())

	return scheme
}

// schemeWithoutClusterPolicy returns a scheme that does not know the
// NetworkClusterPolicy type, which is what makes setting a controller reference
// to a cluster policy fail.
func schemeWithoutClusterPolicy() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(core.AddToScheme(scheme)).To(Succeed())
	Expect(rbac.AddToScheme(scheme)).To(Succeed())
	Expect(apps.AddToScheme(scheme)).To(Succeed())

	return scheme
}

// gaudiPolicy returns a gaudi-so cluster policy asking for a layer 3 setup.
func gaudiPolicy() *networkv1alpha1.NetworkClusterPolicy {
	return &networkv1alpha1.NetworkClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: testGaudiName,
		},
		Spec: networkv1alpha1.NetworkClusterPolicySpec{
			ConfigurationType: gaudiScaleOutSelection,
			GaudiScaleOut: networkv1alpha1.GaudiScaleOutSpec{
				Layer: layerSelectionL3,
			},
			NodeSelector: map[string]string{
				"intel.feature.node.kubernetes.io/gaudi-ready": "true",
			},
		},
	}
}

// The three interceptors below fail the call for objects of the same type as
// the one they are given and pass everything else through. Matching on the type
// keeps an injected failure tied to the call under test, so that the calls the
// reconciler makes for the other objects still reach the client.

// failGetOf fails fetching objects of the type of the given one.
func failGetOf(failFor client.Object) func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
	failType := reflect.TypeOf(failFor)

	return func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if reflect.TypeOf(obj) == failType {
			return errGaudiInjected
		}

		return c.Get(ctx, key, obj, opts...)
	}
}

// failCreateOf fails creating objects of the type of the given one.
func failCreateOf(failFor client.Object) func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
	failType := reflect.TypeOf(failFor)

	return func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if reflect.TypeOf(obj) == failType {
			return errGaudiInjected
		}

		return c.Create(ctx, obj, opts...)
	}
}

// failUpdateOf fails updating objects of the type of the given one.
func failUpdateOf(failFor client.Object) func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
	failType := reflect.TypeOf(failFor)

	return func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
		if reflect.TypeOf(obj) == failType {
			return errGaudiInjected
		}

		return c.Update(ctx, obj, opts...)
	}
}
