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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkv1alpha1 "github.com/intel/network-operator/api/v1alpha1"
)

var _ = Describe("NetworkClusterPolicy Controller", func() {
	const (
		timeout  = time.Second * 5
		duration = time.Second * 5
		interval = time.Millisecond * 250
	)

	defaultNs := "intel-network-operator"

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: defaultNs,
		}
		serviceAccountTypeNamespacedName := types.NamespacedName{
			Name:      resourceName + "-sa",
			Namespace: defaultNs,
		}
		roleBindingTypeNamespacedName := types.NamespacedName{
			Name:      resourceName + "-sa-rb",
			Namespace: defaultNs,
		}

		nicpolicy := &networkv1alpha1.NetworkClusterPolicy{}

		It("should successfully reconcile the resource", func() {
			By("creating the custom resource for the Kind NetworkClusterPolicy")
			resource := &networkv1alpha1.NetworkClusterPolicy{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "intel.com/v1alpha1",
					Kind:       "NetworkClusterPolicy",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: defaultNs,
				},
				Spec: networkv1alpha1.NetworkClusterPolicySpec{
					ConfigurationType: "gaudi-so",
					GaudiScaleOut: networkv1alpha1.GaudiScaleOutSpec{
						Layer:         "L3",
						Image:         "intel/my-linkdiscovery:latest",
						MTU:           8000,
						EnableLLDPAD:  true,
						PFCPriorities: "11110000",
					},
					NodeSelector: map[string]string{
						"foo": "bar",
					},
				},
			}
			ns := &core.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: defaultNs,
				},
			}

			k8sClient.Create(ctx, ns)

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, nicpolicy)).To(Succeed())
				g.Expect(nicpolicy.Spec.ConfigurationType).To(BeEquivalentTo("gaudi-so"))
				g.Expect(nicpolicy.Status.Targets).To(BeIdenticalTo(int32(0)))
				g.Expect(nicpolicy.Status.State).To(BeIdenticalTo("No targets"))
			}, timeout, interval).Should(Succeed())

			var ds apps.DaemonSet
			var sa core.ServiceAccount
			var rb rbac.RoleBinding

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Succeed())
				g.Expect(ds.ObjectMeta.Name).To(BeEquivalentTo(typeNamespacedName.Name))
				g.Expect(ds.Spec.Template.Spec.ServiceAccountName).To(BeEquivalentTo(resourceName + "-sa"))
				g.Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(2))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Image).To(BeEquivalentTo("intel/my-linkdiscovery:latest"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(HaveLen(7))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[0]).To(BeEquivalentTo("--configure=true"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[1]).To(BeEquivalentTo("--keep-running"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[2]).To(BeEquivalentTo("--mode=L3"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[3]).To(BeEquivalentTo("--mtu=8000"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[4]).To(BeEquivalentTo("--wait=90s"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[5]).To(BeEquivalentTo("--gaudinet=/host/etc/habanalabs/gaudinet.json"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[6]).To(BeEquivalentTo("--pfc=0,1,2,3"))
				g.Expect(ds.Spec.Template.Spec.Containers[1].Args).To(HaveLen(0))

				g.Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(3))
				g.Expect(ds.Spec.Template.Spec.Volumes[0].Name).To(BeEquivalentTo("nfd-features"))
				g.Expect(ds.Spec.Template.Spec.Volumes[1].Name).To(BeEquivalentTo("lldpad"))
				g.Expect(ds.Spec.Template.Spec.Volumes[2].Name).To(BeEquivalentTo("gaudinetpath"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(2))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).To(BeEquivalentTo("nfd-features"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts[1].Name).To(BeEquivalentTo("gaudinetpath"))

				g.Expect(ds.Spec.Template.Spec.Containers[1].VolumeMounts).To(HaveLen(2))
				g.Expect(ds.Spec.Template.Spec.Containers[1].VolumeMounts[0].Name).To(BeEquivalentTo("lldpad"))
				g.Expect(ds.Spec.Template.Spec.Containers[1].VolumeMounts[1].Name).To(BeEquivalentTo("lldpad"))

				// Check for service account and role binding
				g.Expect(k8sClient.Get(ctx, serviceAccountTypeNamespacedName, &sa)).To(Succeed())
				g.Expect(k8sClient.Get(ctx, roleBindingTypeNamespacedName, &rb)).To(Succeed())
				g.Expect(rb.Subjects).To(HaveLen(1))
				g.Expect(rb.Subjects[0].Name).To(BeEquivalentTo(resourceName + "-sa"))
				g.Expect(rb.Subjects[0].Namespace).To(BeEquivalentTo(defaultNs))

			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

			resource.Spec.GaudiScaleOut.Layer = "L2"
			resource.Spec.GaudiScaleOut.PFCPriorities = ""

			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Succeed())
				g.Expect(ds.ObjectMeta.Name).To(BeEquivalentTo(typeNamespacedName.Name))
				g.Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(2))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(HaveLen(4))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[0]).To(BeEquivalentTo("--configure=true"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[1]).To(BeEquivalentTo("--keep-running"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[2]).To(BeEquivalentTo("--mode=L2"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[3]).To(BeEquivalentTo("--mtu=8000"))
				g.Expect(ds.Spec.Template.Spec.Containers[1].Args).To(HaveLen(0))
			}, timeout, interval).Should(Succeed())

			// Test NetworkManager disabling
			resource.Spec.GaudiScaleOut.Layer = "L3"
			resource.Spec.GaudiScaleOut.DisableNetworkManager = true
			resource.Spec.GaudiScaleOut.EnableLLDPAD = false
			resource.Spec.GaudiScaleOut.MTU = 0
			resource.Spec.GaudiScaleOut.PFCPriorities = "00000000"

			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Succeed())
				g.Expect(ds.ObjectMeta.Name).To(BeEquivalentTo(typeNamespacedName.Name))
				g.Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(HaveLen(7))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[0]).To(BeEquivalentTo("--configure=true"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[1]).To(BeEquivalentTo("--keep-running"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[2]).To(BeEquivalentTo("--mode=L3"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[3]).To(BeEquivalentTo("--disable-networkmanager"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[4]).To(BeEquivalentTo("--wait=90s"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[5]).To(BeEquivalentTo("--gaudinet=/host/etc/habanalabs/gaudinet.json"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Args[6]).To(BeEquivalentTo("--pfc=none"))

				g.Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(4))
				g.Expect(ds.Spec.Template.Spec.Volumes[0].Name).To(BeEquivalentTo("nfd-features"))
				g.Expect(ds.Spec.Template.Spec.Volumes[1].Name).To(BeEquivalentTo("gaudinetpath"))
				g.Expect(ds.Spec.Template.Spec.Volumes[2].Name).To(BeEquivalentTo("var-run-dbus"))
				g.Expect(ds.Spec.Template.Spec.Volumes[3].Name).To(BeEquivalentTo("networkmanager"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(4))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).To(BeEquivalentTo("nfd-features"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts[1].Name).To(BeEquivalentTo("gaudinetpath"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts[2].Name).To(BeEquivalentTo("var-run-dbus"))
				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts[3].Name).To(BeEquivalentTo("networkmanager"))
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, nicpolicy)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Not(Succeed()))
			}, timeout, interval).Should(Not(Succeed()))

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, nicpolicy)).To(Not(Succeed()))
			}, timeout, interval).Should(Succeed())
		})
	})
})
