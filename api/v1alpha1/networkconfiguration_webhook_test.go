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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("NetworkClusterPolicy Webhook", func() {

	var nc NetworkClusterPolicy

	BeforeEach(func() {
		nc = NetworkClusterPolicy{}
	})

	Context("When creating NetworkClusterPolicy under Defaulting Webhook", func() {
		It("Should fill in the default value if layer 3 is selected with Gaudi", func() {
			nc.Spec.ConfigurationType = gaudiScaleOut
			nc.Spec.GaudiScaleOut.Layer = "L2"

			nc.Default()

			Expect(nc.Spec.GaudiScaleOut.Image).To(BeEquivalentTo("intel/intel-network-linkdiscovery:latest"))
		})

		It("Should fill in the default RDMA device class name if it is empty", func() {
			nc.Spec.ConfigurationType = hostNicScaleOut
			nc.Spec.HostNicScaleOut.InstallDRANet = true
			nc.Spec.HostNicScaleOut.Dranet.RDMADeviceClass = &RDMADeviceClassSpec{}

			nc.Default()

			Expect(nc.Spec.HostNicScaleOut.Dranet.RDMADeviceClass.Name).To(BeEquivalentTo(DefaultRDMADeviceClass))
		})

		It("Should keep an explicitly given RDMA device class name", func() {
			nc.Spec.ConfigurationType = hostNicScaleOut
			nc.Spec.HostNicScaleOut.Dranet.RDMADeviceClass = &RDMADeviceClassSpec{
				Name: "my-device-class",
			}

			nc.Default()

			Expect(nc.Spec.HostNicScaleOut.Dranet.RDMADeviceClass.Name).To(BeEquivalentTo("my-device-class"))
		})

		It("Should not add an RDMA device class if none is requested", func() {
			nc.Spec.ConfigurationType = hostNicScaleOut

			nc.Default()

			Expect(nc.Spec.HostNicScaleOut.Dranet.RDMADeviceClass).To(BeNil())
		})

		It("Should not default the hostnic spec for other configuration types", func() {
			nc.Spec.ConfigurationType = gaudiScaleOut
			nc.Spec.HostNicScaleOut.Dranet.RDMADeviceClass = &RDMADeviceClassSpec{}

			nc.Default()

			Expect(nc.Spec.HostNicScaleOut.Dranet.RDMADeviceClass.Name).To(BeEmpty())
		})
	})

	Context("When creating NetworkClusterPolicy under Validating Webhook", func() {
		It("Should deny if there's no nodeSelector", func() {
			nc.Spec.ConfigurationType = gaudiScaleOut

			Expect(nc.ValidateCreate()).Error().NotTo(BeNil())
		})

		It("Should deny if the configuration type is invalid InputVal", func() {
			nc.Spec.NodeSelector = map[string]string{
				"foo": "bar",
			}

			nc.Spec.ConfigurationType = "foo bar"

			Expect(nc.ValidateCreate()).Error().To(BeEquivalentTo(unknownConfigurationError{}))
		})

		It("Should accept good nodeSelectors", func() {
			nc = NetworkClusterPolicy{
				Spec: NetworkClusterPolicySpec{
					ConfigurationType: gaudiScaleOut,
					GaudiScaleOut: GaudiScaleOutSpec{
						Layer: "L3BGP",
					},
					NodeSelector: map[string]string{},
				},
			}

			goodValues := []map[string]string{
				{"intel.feature.node.kubernetes.io/gaudi-ready": "true"},
				{"gpu.intel.com": "xpu"},
			}

			for _, v := range goodValues {
				nc.Spec.NodeSelector = v

				Expect(nc.ValidateCreate()).Error().To(BeNil(), "selector: %+v", v)
			}
		})

		It("Should prevent bad nodeSelectors InputVal", func() {
			nc = NetworkClusterPolicy{
				Spec: NetworkClusterPolicySpec{
					ConfigurationType: gaudiScaleOut,
					GaudiScaleOut: GaudiScaleOutSpec{
						Layer: "L3",
					},
					NodeSelector: map[string]string{
						"foobar.com?foo": "bar",
					},
				},
			}

			badValues := []map[string]string{
				{"__.com/foo": "bar"},
				{"foo.com_": "bar"},
				{"foo.com": "_bar"},
				{"foo.com": "???foo"},
				{"foo.com": "foo_"},
				{"foo.com": "0123456789012345678901234567890123456789012345678901234567890123"},
				{"foo.com/bar/plaaplaa_": "ok"},
				{"foo.com_/bar": "ok"},
			}

			for _, v := range badValues {
				nc.Spec.NodeSelector = v

				Expect(nc.ValidateCreate()).Error().To(Not(BeNil()), "selector: %+v", v)
			}
		})

		It("Should accept update with good values and fail with bad ones InputVal", func() {
			nc = NetworkClusterPolicy{
				ObjectMeta: v1.ObjectMeta{
					Name: "test",
				},
				Spec: NetworkClusterPolicySpec{
					ConfigurationType: gaudiScaleOut,
					GaudiScaleOut: GaudiScaleOutSpec{
						Layer: "L3",
					},
					NodeSelector: map[string]string{
						"foo": "bar",
					},
				},
			}
			nc2 := nc.DeepCopy()

			Expect(nc2.ValidateUpdate(&nc)).Error().To(BeNil())

			nc2.Spec.NodeSelector = map[string]string{
				"foobar.com?foo": "bar", // bad
			}

			Expect(nc2.ValidateUpdate(&nc)).Error().NotTo(BeNil())
		})

		It("Should always accept delete", func() {
			nc = NetworkClusterPolicy{
				Spec: NetworkClusterPolicySpec{
					ConfigurationType: gaudiScaleOut,
					GaudiScaleOut: GaudiScaleOutSpec{
						Layer: "L3BGP",
					},
					NodeSelector: map[string]string{
						"foo": "bar",
					},
				},
			}

			Expect(nc.ValidateDelete()).Error().To(BeNil())
		})

		It("Should accept a hostnic configuration without a nodeSelector", func() {
			nc = NetworkClusterPolicy{
				Spec: NetworkClusterPolicySpec{
					ConfigurationType: hostNicScaleOut,
					HostNicScaleOut: HostNicScaleOutSpec{
						InstallDRANet: true,
					},
				},
			}

			Expect(nc.ValidateCreate()).Error().To(BeNil())
		})

		It("Should accept a hostnic configuration with a named RDMA device class", func() {
			nc = NetworkClusterPolicy{
				Spec: NetworkClusterPolicySpec{
					ConfigurationType: hostNicScaleOut,
					HostNicScaleOut: HostNicScaleOutSpec{
						InstallDRANet: true,
						Dranet: DranetSpec{
							RDMADeviceClass: &RDMADeviceClassSpec{
								Name: "my-device-class",
							},
						},
					},
				},
			}

			Expect(nc.ValidateCreate()).Error().To(BeNil())
			Expect(nc.ValidateUpdate(nc.DeepCopy())).Error().To(BeNil())
		})

		It("Should deny a hostnic configuration with an unnamed RDMA device class InputVal", func() {
			nc = NetworkClusterPolicy{
				Spec: NetworkClusterPolicySpec{
					ConfigurationType: hostNicScaleOut,
					HostNicScaleOut: HostNicScaleOutSpec{
						InstallDRANet: true,
						Dranet: DranetSpec{
							RDMADeviceClass: &RDMADeviceClassSpec{},
						},
					},
				},
			}

			Expect(nc.ValidateCreate()).Error().To(BeEquivalentTo(missingDeviceClassNameError{}))
			Expect(missingDeviceClassNameError{}.Error()).NotTo(BeEmpty())

			// Defaulting gives the device class a name, making the spec valid.
			nc.Default()

			Expect(nc.ValidateCreate()).Error().To(BeNil())
		})
	})
})
