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
	resourceApi "k8s.io/apimachinery/pkg/api/resource"
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

			expectedArgs := []string{
				"--configure=true",
				"--keep-running",
				"--mode=L3",
				"--mtu=8000",
				"--wait=90s",
				"--gaudinet=/host/etc/habanalabs/gaudinet.json",
				"--pfc=0,1,2,3",
			}

			expectedVolumes := []string{
				"nfd-features",
				"lldpad",
				"gaudinetpath",
			}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Succeed())
				g.Expect(ds.ObjectMeta.Name).To(BeEquivalentTo(typeNamespacedName.Name))
				g.Expect(ds.Spec.Template.Spec.ServiceAccountName).To(BeEquivalentTo(resourceName + "-sa"))
				g.Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(2))
				g.Expect(ds.Spec.Template.Spec.Containers[0].Image).To(BeEquivalentTo("intel/my-linkdiscovery:latest"))

				verified := map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(HaveLen(len(expectedArgs)))
				for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
					g.Expect(expectedArgs).To(ContainElement(arg))
					verified[arg] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedArgs)))

				g.Expect(ds.Spec.Template.Spec.Containers[1].Args).To(HaveLen(0))

				verified = map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(len(expectedVolumes)))
				for _, vol := range ds.Spec.Template.Spec.Volumes {
					g.Expect(expectedVolumes).To(ContainElement(vol.Name))
					verified[vol.Name] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedVolumes)))

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

			expectedArgs = []string{
				"--configure=true",
				"--keep-running",
				"--mode=L2",
				"--mtu=8000",
			}

			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Succeed())
				g.Expect(ds.ObjectMeta.Name).To(BeEquivalentTo(typeNamespacedName.Name))
				g.Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(2))

				verified := map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(HaveLen(len(expectedArgs)))
				for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
					g.Expect(expectedArgs).To(ContainElement(arg))
					verified[arg] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedArgs)))

				g.Expect(ds.Spec.Template.Spec.Containers[1].Args).To(HaveLen(0))
			}, timeout, interval).Should(Succeed())

			// Test NetworkManager disabling
			resource.Spec.GaudiScaleOut.Layer = "L3"
			resource.Spec.GaudiScaleOut.DisableNetworkManager = true
			resource.Spec.GaudiScaleOut.EnableLLDPAD = false
			resource.Spec.GaudiScaleOut.MTU = 0
			resource.Spec.GaudiScaleOut.PFCPriorities = "00000000"
			resource.Spec.GaudiScaleOut.NetworkMetrics = true

			expectedArgs = []string{
				"--configure=true",
				"--keep-running",
				"--mode=L3",
				"--disable-networkmanager",
				"--wait=90s",
				"--gaudinet=/host/etc/habanalabs/gaudinet.json",
				"--pfc=none",
				"--metrics-bind-address=:50152",
			}

			// Same names are also used for volume mounts
			expectedVolumes = []string{
				"nfd-features",
				"var-run-dbus",
				"networkmanager",
				"gaudinetpath",
			}

			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Succeed())
				g.Expect(ds.ObjectMeta.Name).To(BeEquivalentTo(typeNamespacedName.Name))
				g.Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))

				verified := map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(HaveLen(len(expectedArgs)))
				for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
					g.Expect(expectedArgs).To(ContainElement(arg))
					verified[arg] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedArgs)))

				g.Expect(ds.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(1))

				verified = map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(len(expectedVolumes)))
				for _, vol := range ds.Spec.Template.Spec.Volumes {
					g.Expect(expectedVolumes).To(ContainElement(vol.Name))
					verified[vol.Name] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedVolumes)))

				verified = map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(len(expectedVolumes)))
				for _, vol := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
					g.Expect(expectedVolumes).To(ContainElement(vol.Name))
					verified[vol.Name] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedVolumes)))
			}, timeout, interval).Should(Succeed())

			resource.Spec.GaudiScaleOut.EnableLLDPAD = true
			resource.Spec.GaudiScaleOut.NetworkMetrics = false

			expectedArgs = []string{
				"--configure=true",
				"--keep-running",
				"--mode=L3",
				"--disable-networkmanager",
				"--wait=90s",
				"--gaudinet=/host/etc/habanalabs/gaudinet.json",
				"--pfc=none",
			}

			expectedVolumes = []string{
				"nfd-features",
				"var-run-dbus",
				"networkmanager",
				"gaudinetpath",
				"lldpad",
			}

			expectedVolMountsC0 := []string{
				"nfd-features",
				"var-run-dbus",
				"networkmanager",
				"gaudinetpath",
			}

			expectedVolMountPathsC1 := []string{
				"/var/lib/lldpad", "/var/run",
			}

			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &ds)).To(Succeed())
				g.Expect(ds.ObjectMeta.Name).To(BeEquivalentTo(typeNamespacedName.Name))
				g.Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(2))

				verified := map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(HaveLen(len(expectedArgs)))
				for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
					g.Expect(expectedArgs).To(ContainElement(arg))
					verified[arg] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedArgs)))

				g.Expect(ds.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(0))

				verified = map[string]struct{}{}
				g.Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(len(expectedVolumes)))
				for _, vol := range ds.Spec.Template.Spec.Volumes {
					g.Expect(expectedVolumes).To(ContainElement(vol.Name))
					verified[vol.Name] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedVolumes)))

				g.Expect(*ds.Spec.Template.Spec.Volumes[4].VolumeSource.EmptyDir.SizeLimit).To(BeEquivalentTo(resourceApi.MustParse(emptyDirSize)))

				verified = map[string]struct{}{}

				g.Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(len(expectedVolMountsC0)))
				for _, vol := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
					g.Expect(expectedVolMountsC0).To(ContainElement(vol.Name))
					verified[vol.Name] = struct{}{}
				}

				g.Expect(verified).To(HaveLen(len(expectedVolMountsC0)))

				g.Expect(ds.Spec.Template.Spec.Containers[1].VolumeMounts).To(HaveLen(len(expectedVolMountPathsC1)))
				for _, vol := range ds.Spec.Template.Spec.Containers[1].VolumeMounts {
					g.Expect(vol.Name).To(BeEquivalentTo("lldpad"))
					g.Expect(expectedVolMountPathsC1).To(ContainElement(vol.MountPath))
				}

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
