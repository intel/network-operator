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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const (
	driverPath       = "bus/pci/drivers/habanalabs/"
	pciDevicePattern = "????:??:??.?"
	netDevicePattern = "net/*"

	noAddress = "none"
)

type networkLinkFn struct {
	LinkByName    func(name string) (netlink.Link, error)
	AddrList      func(link netlink.Link, family int) ([]netlink.Addr, error)
	AddrAdd       func(link netlink.Link, addr *netlink.Addr) error
	LinkSubscribe func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error
}

var networkLink = networkLinkFn{
	LinkByName:    netlink.LinkByName,
	AddrList:      netlink.AddrList,
	AddrAdd:       netlink.AddrAdd,
	LinkSubscribe: netlink.LinkSubscribe,
}

type networkConfiguration struct {
	link            netlink.Link
	origState       net.Flags
	expectResponse  bool
	portDescription string
	lldpPeer        *net.IP
	localAddr       *net.IP
	peerHWAddr      *net.HardwareAddr
	localHwAddr     *net.HardwareAddr
}

func getSysfsRoot() string {
	sysfsRoot := os.Getenv("SYSFS_ROOT")
	if sysfsRoot == "" {
		sysfsRoot = "/sys/"
	}
	return sysfsRoot
}

func sysfsDriverPath() string {
	return filepath.Join(getSysfsRoot(), driverPath)
}

func getNetworks() []string {
	habanaNetDevices := []string{}

	pattern := filepath.Join(sysfsDriverPath(), pciDevicePattern)
	paths, err := filepath.Glob(pattern)

	if err != nil {
		klog.Warningf("no PCI devices found\n")
		return habanaNetDevices
	}

	for _, p := range paths {
		devicesymlinktarget, err := filepath.EvalSymlinks(p)
		if err != nil {
			klog.Warningf("Expected '%s' to be a symlink: %v\n", p, err)
			continue
		}

		netdevicepattern := filepath.Join(devicesymlinktarget, netDevicePattern)
		netdevices, err := filepath.Glob(netdevicepattern)
		if err != nil {
			klog.Warningf("Could not find network device files: %v", err)
		}
		for _, n := range netdevices {
			name := filepath.Base(n)
			habanaNetDevices = append(habanaNetDevices, name)
		}

	}

	return habanaNetDevices
}

func getNetworkConfigs(ifacenames []string) map[string]*networkConfiguration {
	links := make(map[string]*networkConfiguration)

	for _, name := range ifacenames {
		link, err := networkLink.LinkByName(name)
		if err != nil {
			klog.Warningf("Link '%s' not found: %v\n", name, err)
			continue
		}

		links[name] = &networkConfiguration{
			link:        link,
			origState:   link.Attrs().Flags,
			localHwAddr: &link.Attrs().HardwareAddr,
		}
	}

	return links
}

func selectMask30L3Address(nwconfig *networkConfiguration) (*net.IP, *net.IP, error) {
	var (
		peerNetwork *net.IPNet
		peeraddr    net.IP
		localaddr   net.IP
		err         error
	)

	substrings := strings.Split(nwconfig.portDescription, " ")
	if len(substrings) < 2 {
		return nil, nil, fmt.Errorf("interface '%s' could not split string '%s'",
			nwconfig.link.Attrs().Name, nwconfig.portDescription)
	}

	peeraddr, peerNetwork, err = net.ParseCIDR(substrings[1])
	if err != nil {
		return nil, nil, fmt.Errorf("interface '%s' could not parse '%s': %v",
			nwconfig.link.Attrs().Name, nwconfig.portDescription, err)
	}

	mask, _ := peerNetwork.Mask.Size()
	if mask == 30 {
		// toggle the lowest two bits of the switch IPv4 address to get
		// the local address
		peer := peeraddr.To4()
		localaddr = net.IPv4(peer[0], peer[1], peer[2], peer[3]^0x3)
	} else {
		err = fmt.Errorf("Mask is %d, not the expected 30", mask)
	}

	return &peeraddr, &localaddr, err
}

func logResults(config *cmdConfig, networkConfigs map[string]*networkConfiguration) {
	for _, nwconfig := range networkConfigs {
		klog.V(3).Infof("Interface '%s' %s:\n", nwconfig.link.Attrs().Name, nwconfig.link.Attrs().Flags)

		str := ("\tConfigured addresses: ")
		addrs, err := networkLink.AddrList(nwconfig.link, netlink.FAMILY_ALL)
		if len(addrs) == 0 || err != nil {
			str += "no addresses"
		} else {
			for _, addr := range addrs {
				str += addr.IPNet.String()
				if nwconfig.localAddr != nil && addr.IPNet.IP.Equal(*nwconfig.localAddr) {
					str += "(matches lldp)"
				}
				str += " "
			}
		}
		klog.V(3).Info(str)

		if config.mode == L3 {
			addr := noAddress
			if nwconfig.peerHWAddr != nil {
				addr = nwconfig.peerHWAddr.String()
			}
			klog.V(3).Infof("\tPeer MAC address: %s\n", addr)

			addr = noAddress
			if nwconfig.lldpPeer != nil {
				addr = nwconfig.lldpPeer.String()
			}
			klog.V(3).Infof("\tLLDP address: %s\n", addr)
			addr = noAddress
			if nwconfig.localAddr != nil {
				addr = nwconfig.localAddr.String()
			}
			klog.V(3).Infof("\tLLDP /30 address to add: %s\n", addr)
		}
	}
}

func lldpResults(networkConfigs map[string]*networkConfiguration) bool {
	foundpeers := false

	for _, nwconfig := range networkConfigs {
		lldpPeer, localAddr, err := selectMask30L3Address(nwconfig)
		if err == nil {
			nwconfig.lldpPeer = lldpPeer
			nwconfig.localAddr = localAddr
			foundpeers = true
		}
	}

	return foundpeers
}

func allLinksResponded(networkConfigs map[string]*networkConfiguration) bool {
	for _, nwconfig := range networkConfigs {
		if nwconfig.expectResponse {
			return false
		}
	}
	return true
}

func waitLinkResponse(linkUpdate chan netlink.LinkUpdate, networkConfigs map[string]*networkConfiguration) error {
	for !allLinksResponded(networkConfigs) {
		select {
		case update := <-linkUpdate:
			if nwconfig, exists := networkConfigs[update.Link.Attrs().Name]; exists {
				nwconfig.link = update.Link
				nwconfig.expectResponse = false
			}

		case <-time.After(3 * time.Second):
			return fmt.Errorf("timeout waiting for netlink reply")
		}
	}

	return nil
}

func interfacesUp(networkConfigs map[string]*networkConfiguration) error {
	linkUpdate := make(chan netlink.LinkUpdate)
	done := make(chan struct{})
	defer close(done)

	if err := networkLink.LinkSubscribe(linkUpdate, done); err != nil {
		return err
	}

	for _, nwconfig := range networkConfigs {
		nwconfig.expectResponse = false
		if nwconfig.link.Attrs().Flags&net.FlagUp == 0 {
			if err := netlink.LinkSetUp(nwconfig.link); err == nil {
				nwconfig.expectResponse = true
			} else {
				klog.Warningf("Cannot set link '%s' up: %v\n", nwconfig.link.Attrs().Name, err)
				continue
			}
		}
	}

	_ = waitLinkResponse(linkUpdate, networkConfigs)

	return nil
}

func interfacesRestoreDown(networkConfigs map[string]*networkConfiguration) error {
	linkUpdate := make(chan netlink.LinkUpdate)
	done := make(chan struct{})
	defer close(done)

	subscribeErr := networkLink.LinkSubscribe(linkUpdate, done)

	for _, nwconfig := range networkConfigs {
		if nwconfig.origState&net.FlagUp == 0 && nwconfig.link.Attrs().Flags&net.FlagUp != 0 {
			if err := netlink.LinkSetDown(nwconfig.link); err == nil {
				klog.Infof("Setting link '%s' back down\n", nwconfig.link.Attrs().Name)
			} else {
				klog.Warningf("Cannot set link '%s' back down: %v\n", nwconfig.link.Attrs().Name, err)
			}
		}
	}

	if subscribeErr != nil {
		return subscribeErr
	}

	_ = waitLinkResponse(linkUpdate, networkConfigs)

	return nil
}

func configureInterfaces(networkConfigs map[string]*networkConfiguration) (int, int) {
	configured := 0

	klog.Infof("Configuring interfaces...\n")

	for _, nwconfig := range networkConfigs {
		if nwconfig.localAddr == nil {
			continue
		}

		addrs, err := networkLink.AddrList(nwconfig.link, netlink.FAMILY_V4)
		ifname := nwconfig.link.Attrs().Name
		if err != nil {
			klog.Warningf("Could not get addresses for link '%s': %v\n", ifname, err)
			continue
		}

		foundExisting := false

		for _, addr := range addrs {
			if nwconfig.localAddr.Equal(addr.IPNet.IP) {
				klog.Infof("Interface '%s' already configured '%s'\n",
					ifname, addr.IPNet.String())

				foundExisting = true
				configured++

				break
			}
		}

		if !foundExisting {
			newlinkaddr := &netlink.Addr{
				IPNet: &net.IPNet{
					IP:   *nwconfig.localAddr,
					Mask: net.CIDRMask(30, 32),
				},
			}
			if err := networkLink.AddrAdd(nwconfig.link, newlinkaddr); err != nil {
				klog.Warningf("Could not configure address '%s' to interface '%s': %v\n",
					nwconfig.localAddr.String(), ifname, err)
				continue
			}
			configured++

			klog.Infof("Configured address '%s' to interface '%s'\n",
				nwconfig.localAddr.String(), ifname)
		}
	}

	return configured, len(networkConfigs)
}
