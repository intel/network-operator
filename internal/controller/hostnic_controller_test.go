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
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	networkv1alpha1 "github.com/intel/network-operator/api/v1alpha1"
	"github.com/intel/network-operator/config/deployments"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace = "foobar"
)

var _ = Describe("DRANet Controller", func() {

	Context("Verify object reconciliations", func() {

		It("Verify k8s object handling", func() {
			gaudi_cp := &networkv1alpha1.NetworkClusterPolicy{
				Spec: networkv1alpha1.NetworkClusterPolicySpec{
					ConfigurationType: "gaudi-so",
				},
			}
			cp := &networkv1alpha1.NetworkClusterPolicy{
				Spec: networkv1alpha1.NetworkClusterPolicySpec{
					ConfigurationType: "hostnic-so",
				},
			}

			scheme := runtime.NewScheme()
			Expect(core.AddToScheme(scheme)).To(Succeed())
			Expect(rbac.AddToScheme(scheme)).To(Succeed())
			Expect(apps.AddToScheme(scheme)).To(Succeed())
			Expect(networkv1alpha1.AddToScheme(scheme)).To(Succeed())

			r := HostNICReconciler{Scheme: scheme, Namespace: testNamespace}
			r.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(cp).Build()

			// ClusterRole
			expectedClusterRole := deployments.DranetClusterRole()

			createdClusterRole := rbac.ClusterRole{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedClusterRole.Name}, &createdClusterRole)).To(HaveOccurred())

			Expect(r.updateClusterRole(ctx, cp)).To(BeNil())
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedClusterRole.Name}, &createdClusterRole)).NotTo(HaveOccurred())
			Expect(expectedClusterRole.Name).To(Equal(createdClusterRole.Name))
			Expect(cmp.Diff(expectedClusterRole.Rules, createdClusterRole.Rules, cmpopts.EquateEmpty())).To(Equal(""))

			Expect(r.updateClusterRole(ctx, cp)).NotTo(HaveOccurred())
			updatedClusterRole := rbac.ClusterRole{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedClusterRole.Name}, &updatedClusterRole)).NotTo(HaveOccurred())
			Expect(cmp.Diff(createdClusterRole, updatedClusterRole, cmpopts.EquateEmpty())).To(Equal(""))

			// ClusterRoleBinding
			expectedClusterRoleBinding := deployments.DranetClusterRoleBinding()
			for i := range expectedClusterRoleBinding.Subjects {
				if expectedClusterRoleBinding.Subjects[i].Kind == rbac.ServiceAccountKind {
					expectedClusterRoleBinding.Subjects[i].Namespace = testNamespace
				}
			}

			createdClusterRoleBinding := rbac.ClusterRoleBinding{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedClusterRoleBinding.Name}, &createdClusterRoleBinding)).To(HaveOccurred())

			Expect(r.updateClusterRoleBinding(ctx, cp)).To(BeNil())
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedClusterRoleBinding.Name}, &createdClusterRoleBinding)).NotTo(HaveOccurred())
			Expect(expectedClusterRoleBinding.Name).To(Equal(createdClusterRoleBinding.Name))
			Expect(cmp.Diff(expectedClusterRoleBinding.RoleRef, createdClusterRoleBinding.RoleRef, cmpopts.EquateEmpty())).To(Equal(""))
			Expect(cmp.Diff(expectedClusterRoleBinding.Subjects, createdClusterRoleBinding.Subjects, cmpopts.EquateEmpty())).To(Equal(""))

			Expect(r.updateClusterRoleBinding(ctx, cp)).NotTo(HaveOccurred())
			updatedClusterRoleBinding := rbac.ClusterRoleBinding{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedClusterRoleBinding.Name}, &updatedClusterRoleBinding)).NotTo(HaveOccurred())
			Expect(cmp.Diff(createdClusterRoleBinding, updatedClusterRoleBinding, cmpopts.EquateEmpty())).To(Equal(""))

			// ServiceAccount
			expectedServiceAccount := deployments.DranetServiceAccount()
			expectedServiceAccount.Namespace = testNamespace

			createdServiceAccount := core.ServiceAccount{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedServiceAccount.Name, Namespace: testNamespace}, &createdServiceAccount)).To(HaveOccurred())

			Expect(r.updateServiceAccount(ctx, cp)).To(BeNil())
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedServiceAccount.Name, Namespace: testNamespace}, &createdServiceAccount)).NotTo(HaveOccurred())
			Expect(expectedServiceAccount.Name).To(Equal(createdServiceAccount.Name))
			Expect(testNamespace).To(Equal(createdServiceAccount.Namespace))

			Expect(r.updateServiceAccount(ctx, cp)).NotTo(HaveOccurred())
			updatedServiceAccount := core.ServiceAccount{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedServiceAccount.Name, Namespace: testNamespace}, &updatedServiceAccount)).NotTo(HaveOccurred())
			Expect(cmp.Diff(createdServiceAccount, updatedServiceAccount, cmpopts.EquateEmpty())).To(Equal(""))

			// DaemonSet
			expectedDaemonSet := deployments.DranetDaemonSet()
			expectedDaemonSet.Namespace = testNamespace

			createdDaemonSet := apps.DaemonSet{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedDaemonSet.Name, Namespace: testNamespace}, &createdDaemonSet)).To(HaveOccurred())

			Expect(r.updateDranetDaemonSet(ctx, cp)).To(BeNil())
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedDaemonSet.Name, Namespace: testNamespace}, &createdDaemonSet)).NotTo(HaveOccurred())
			Expect(expectedDaemonSet.Name).To(Equal(createdDaemonSet.Name))
			Expect(testNamespace).To(Equal(createdDaemonSet.Namespace))
			Expect(cmp.Diff(createdDaemonSet.Spec, expectedDaemonSet.Spec, cmpopts.EquateEmpty())).To(Equal(""))

			Expect(r.updateDranetDaemonSet(ctx, cp)).NotTo(HaveOccurred())
			updatedDaemonSet := apps.DaemonSet{}
			Expect(r.Get(ctx, client.ObjectKey{Name: expectedDaemonSet.Name, Namespace: testNamespace}, &updatedDaemonSet)).NotTo(HaveOccurred())
			Expect(cmp.Diff(createdDaemonSet, updatedDaemonSet, cmpopts.EquateEmpty())).To(Equal(""))

			// Reconciles that should not pass
			_, err := r.Reconcile(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, gaudi_cp)
			Expect(err).NotTo(HaveOccurred())

		})
	})
})
