/*
 * Copyright (C) 2024 Intel Corporation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/klog/v2"
)

type GaudiNet struct {
	Config []GaudiNetEntry `json:"NIC_NET_CONFIG"`
}

type GaudiNetEntry struct {
	Mac        string `json:"NIC_MAC"`
	IP         string `json:"NIC_IP"`
	Mask       string `json:"SUBNET_MASK"`
	GatewayMac string `json:"GATEWAY_MAC"`
}

var (
	JsonMarshal func(v any) ([]byte, error)                                      = json.Marshal
	JsonIndent  func(dst *bytes.Buffer, src []byte, prefix, indent string) error = json.Indent
)

func GenerateGaudiNet(networkConfigs map[string]*networkConfiguration) ([]byte, error) {
	gaudinet := &GaudiNet{Config: []GaudiNetEntry{}}

	for ifname, nwconfig := range networkConfigs {
		if nwconfig.localAddr == nil {
			klog.Warningf("Interface '%s' has no LLDP address when creating gaudinet file, skipping...\n", ifname)
			continue
		}

		if nwconfig.peerHWAddr == nil {
			klog.Warningf("Interface '%s' has no peer MAC address when creating gaudinet file, skipping...\n", ifname)
			continue
		}

		net := GaudiNetEntry{
			Mac:        nwconfig.link.Attrs().HardwareAddr.String(),
			IP:         nwconfig.localAddr.String(),
			Mask:       "255.255.255.252",
			GatewayMac: nwconfig.peerHWAddr.String(),
		}

		gaudinet.Config = append(gaudinet.Config, net)
	}

	gaudinetJSON, err := JsonMarshal(gaudinet)
	if err != nil {
		return nil, fmt.Errorf("Could not marshal json: %v", err)
	}
	gaudinetJSON = append(gaudinetJSON, '\n')

	var gaudinetContents bytes.Buffer
	if err := JsonIndent(&gaudinetContents, gaudinetJSON, "", "  "); err != nil {
		return nil, err
	}

	return gaudinetContents.Bytes(), nil
}

func WriteGaudiNet(filename string, networkConfigs map[string]*networkConfiguration) error {
	if filename == "" {
		return fmt.Errorf("no file name when saving gaudinet.json")
	}

	gaudinetContents, err := GenerateGaudiNet(networkConfigs)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, gaudinetContents, 0644)
}
