/*
 * Copyright (C) 2025 Intel Corporation
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
	"fmt"
	"net"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"
)

const (
	SystemdNetworkdPath = "/etc/systemd/network"
)

func networkdFilename(networkdpath string, ifname string) string {
	return filepath.Join(networkdpath, ifname+".network")
}

func checkNetworkConfig(ifname string, nwconfig *networkConfiguration) error {
	if nwconfig.link == nil {
		return fmt.Errorf("no link information for %s", ifname)
	}

	if nwconfig.localAddr == nil {
		return fmt.Errorf("no local address for %s", ifname)
	}

	if nwconfig.link.Attrs().HardwareAddr.String() == "" {
		return fmt.Errorf("no local hw address for %s", ifname)
	}
	return nil
}

func writeNetwork(networkdpath string, ifname string, nwconfig *networkConfiguration) error {
	networkMask := net.CIDRMask(int(RouteMaskRoutedNetwork), 32)
	networkAddr := nwconfig.localAddr.Mask(networkMask)

	network := fmt.Sprintf("[Match]\n"+
		"MACAddress=%s\n"+
		"\n"+
		"[Network]\n"+
		"Description=Networkd configuration for %s created by network-operator\n"+
		"Address=%s/%d\n"+
		"\n"+
		"[Route]\n"+
		"Destination=%s/%d\n",
		nwconfig.link.Attrs().HardwareAddr.String(),
		ifname,
		nwconfig.localAddr.String(), int(RouteMaskPointToPoint),
		networkAddr, int(RouteMaskRoutedNetwork),
	)

	filename := networkdFilename(networkdpath, ifname)
	if err := os.WriteFile(filename, []byte(network), 0644); err != nil {
		return fmt.Errorf("could not write networkd config file '%s': %v", filename, err)
	}

	return nil
}

func WriteSystemdNetworkd(networkdpath string, networkConfigs map[string]*networkConfiguration) ([]string, error) {
	configured := []string{}

	for ifname, nwconfig := range networkConfigs {
		if err := checkNetworkConfig(ifname, nwconfig); err != nil {
			return nil, err
		}
	}

	for ifname, nwconfig := range networkConfigs {
		if err := writeNetwork(networkdpath, ifname, nwconfig); err != nil {
			DeleteSystemdNetworkd(networkdpath, configured)
			return nil, err
		}
		configured = append(configured, ifname)
	}

	return configured, nil
}

func DeleteSystemdNetworkd(networkdpath string, configuredInterfaces []string) {
	for _, ifname := range configuredInterfaces {
		filename := networkdFilename(networkdpath, ifname)
		if err := os.Remove(filename); err != nil {
			klog.Warningf("Failed to remove systemd-networkd file '%s': %v\n", filename, err)
		}
	}
}
