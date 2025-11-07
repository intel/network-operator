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
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

const (
	driverPath         = "bus/pci/drivers/habanalabs/"
	pciDevicePattern   = "????:??:??.?"
	netDevicePattern   = "net/*"
	accelDevicePath    = "class/accel"
	accelDevicePattern = "accel[0-9]*"
	accelDeviceDir     = "device"
	accelModuleIdFile  = "module_id"
	accelPCIDeviceFile = "pci_addr"
	noAddress          = "none"
)

type networkLinkFn struct {
	LinkByName    func(name string) (netlink.Link, error)
	AddrList      func(link netlink.Link, family int) ([]netlink.Addr, error)
	AddrAdd       func(link netlink.Link, addr *netlink.Addr) error
	AddrDel       func(link netlink.Link, addr *netlink.Addr) error
	LinkSubscribe func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error
	RouteAppend   func(route *netlink.Route) error
	LinkSetUp     func(link netlink.Link) error
	LinkSetDown   func(link netlink.Link) error
	LinkSetMTU    func(link netlink.Link, mtu int) error
}

var networkLink = networkLinkFn{
	LinkByName:    netlink.LinkByName,
	AddrList:      netlink.AddrList,
	AddrAdd:       netlink.AddrAdd,
	AddrDel:       netlink.AddrDel,
	LinkSubscribe: netlink.LinkSubscribe,
	RouteAppend:   netlink.RouteAppend,
	LinkSetUp:     netlink.LinkSetUp,
	LinkSetDown:   netlink.LinkSetDown,
	LinkSetMTU:    netlink.LinkSetMTU,
}

type networkConfiguration struct {
	moduleId        string
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

func sysfsClassAccelPath() string {
	return filepath.Join(getSysfsRoot(), accelDevicePath)
}

func getModuleIds() (map[string]string, error) {
	moduleIds := make(map[string]string)

	pattern := filepath.Join(sysfsClassAccelPath(), accelDevicePattern)
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, path := range paths {
		id, err := os.ReadFile(filepath.Join(path, accelDeviceDir, accelModuleIdFile))
		if err != nil {
			klog.Warningf("no module ID found in %s", path)

			continue
		}

		pciaddr, err := os.ReadFile(filepath.Join(path, accelDeviceDir, accelPCIDeviceFile))
		if err != nil {
			klog.Warningf("no PCI address file in %s", path)

			continue
		}

		trimPCI := strings.TrimSpace(string(pciaddr))
		trimID := strings.TrimSpace(string(id))

		klog.V(3).Infof("PCI address %s has module ID: %s", trimPCI, trimID)

		moduleIds[trimPCI] = trimID
	}

	return moduleIds, nil
}

func newNetworkConfiguration(ifname string, moduleId string) (*networkConfiguration, error) {
	link, err := networkLink.LinkByName(ifname)
	if err != nil {
		return nil, fmt.Errorf("Link '%s' not found: %v", ifname, err)
	}

	return &networkConfiguration{
		moduleId:    moduleId,
		link:        link,
		origState:   link.Attrs().Flags,
		localHwAddr: &link.Attrs().HardwareAddr,
	}, nil
}

func getNetworkConfigs(interfaces []string) ([]string, map[string]*networkConfiguration, error) {
	links := make(map[string]*networkConfiguration)
	moduleIds := make(map[string]string)

	pattern := filepath.Join(sysfsDriverPath(), pciDevicePattern)
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, nil, err
	}

	if len(paths) > 0 {
		var err error

		moduleIds, err = getModuleIds()
		if err != nil {
			return nil, nil, err
		}
	} else {
		klog.Warningf("No habanalabs kernel driver PCI devices found")
	}

	allinterfaces := paths

	for _, p := range paths {
		pciaddr := filepath.Base(p)
		id, exists := moduleIds[pciaddr]
		if !exists {
			klog.Warningf("PCI device '%s' does not have a module id", pciaddr)
		}

		devicesymlinktarget, err := filepath.EvalSymlinks(p)
		if err != nil {
			return nil, nil, fmt.Errorf("Expected '%s' to be a symlink: %v", p, err)
		}

		netdevicepattern := filepath.Join(devicesymlinktarget, netDevicePattern)
		netdevices, err := filepath.Glob(netdevicepattern)
		if err != nil {
			return nil, nil, fmt.Errorf("Could not find network device files: %v", err)
		}

		for _, n := range netdevices {
			ifname := filepath.Base(n)
			nwconfig, err := newNetworkConfiguration(ifname, id)
			if err != nil {
				return nil, nil, err
			}
			links[ifname] = nwconfig
		}

	}

	for _, ifname := range interfaces {
		if _, exists := links[ifname]; exists {
			klog.Warningf("Device '%s' already discovered", ifname)
			continue
		}

		nwconfig, err := newNetworkConfiguration(ifname, "")
		if err != nil {
			return nil, nil, err
		}
		links[ifname] = nwconfig

		allinterfaces = append(allinterfaces, ifname)
	}

	return allinterfaces, links, nil
}

func parseIPFromString(portDescription string) (net.IP, *net.IPNet, error) {
	substrings := strings.Split(portDescription, " ")
	if len(substrings) < 2 {
		return nil, nil, fmt.Errorf("port description field doesn't follow expected format (no-alert CIDR)")
	}

	return net.ParseCIDR(substrings[1])
}

func selectMask30L3Address(nwconfig *networkConfiguration) (*net.IP, *net.IP, error) {
	var (
		peerNetwork *net.IPNet
		peeraddr    net.IP
		localaddr   net.IP
		err         error
	)

	peeraddr, peerNetwork, err = parseIPFromString(nwconfig.portDescription)
	if err != nil {
		return nil, nil, fmt.Errorf("interface '%s' could not parse CIDR from port description '%s': %v",
			nwconfig.link.Attrs().Name, nwconfig.portDescription, err)
	}

	mask, _ := peerNetwork.Mask.Size()
	if mask == 30 {
		// toggle the lowest two bits of the switch IPv4 address to get
		// the local address
		peer := peeraddr.To4()
		localaddr = net.IPv4(peer[0], peer[1], peer[2], peer[3]^0x3)
	} else {
		err = fmt.Errorf("interface '%s' mask is %d, not the expected 30",
			nwconfig.link.Attrs().Name, mask)
	}

	return &peeraddr, &localaddr, err
}

func logResults(config *cmdConfig, networkConfigs map[string]*networkConfiguration) {
	for _, nwconfig := range networkConfigs {
		klog.V(3).Infof("Interface '%s' %s:", nwconfig.link.Attrs().Name, nwconfig.link.Attrs().Flags)

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
			klog.V(3).Infof("\tPeer MAC address: %s", addr)

			addr = noAddress
			if nwconfig.lldpPeer != nil {
				addr = nwconfig.lldpPeer.String()
			}
			klog.V(3).Infof("\tPeer LLDP address: %s", addr)
			addr = noAddress
			if nwconfig.localAddr != nil {
				addr = nwconfig.localAddr.String()
			}
			klog.V(3).Infof("\tLocal /30 LLDP address: %s", addr)
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
		} else {
			klog.Warning(err.Error())
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
			if err := networkLink.LinkSetUp(nwconfig.link); err == nil {
				nwconfig.expectResponse = true
			} else {
				klog.Warningf("Cannot set link '%s' up: %v", nwconfig.link.Attrs().Name, err)
				continue
			}
		}
	}

	if err := waitLinkResponse(linkUpdate, networkConfigs); err != nil {
		return err
	}

	return nil
}

func interfacesRestoreDown(networkConfigs map[string]*networkConfiguration) error {
	linkUpdate := make(chan netlink.LinkUpdate)
	done := make(chan struct{})
	defer close(done)

	subscribeErr := networkLink.LinkSubscribe(linkUpdate, done)

	for _, nwconfig := range networkConfigs {
		if nwconfig.origState&net.FlagUp == 0 && nwconfig.link.Attrs().Flags&net.FlagUp != 0 {
			if err := networkLink.LinkSetDown(nwconfig.link); err == nil {
				klog.Infof("Setting link '%s' back down", nwconfig.link.Attrs().Name)
			} else {
				klog.Warningf("Cannot set link '%s' back down: %v", nwconfig.link.Attrs().Name, err)
			}
		}
	}

	if subscribeErr != nil {
		return subscribeErr
	}

	if err := waitLinkResponse(linkUpdate, networkConfigs); err != nil {
		return err
	}

	return nil
}

type RouteMask int

const (
	RouteMaskRoutedNetwork RouteMask = 16
	RouteMaskPointToPoint  RouteMask = 30
)

func addRoute(nwconfig *networkConfiguration, mask RouteMask) error {
	var (
		err             error
		networkSrc      net.IP
		networkGateway  net.IP
		networkScope    netlink.Scope
		networkProtocol netlink.RouteProtocol
		routeStr        string
	)

	networkMask := net.CIDRMask(int(mask), 32)
	if nwconfig.localAddr == nil {
		return fmt.Errorf("interface '%s' has no local address", nwconfig.link.Attrs().Name)
	}
	networkAddr := nwconfig.localAddr.Mask(networkMask)

	switch mask {
	case RouteMaskRoutedNetwork:
		// no protocol set in order to be identical to previous
		// configuration
		networkGateway = *nwconfig.lldpPeer
		routeStr = " gateway " + networkGateway.String()

	case RouteMaskPointToPoint:
		// use protocol 'kernel' to create an identical /30 route as
		// added by the kernel
		networkProtocol = unix.RTPROT_KERNEL
		networkScope = netlink.SCOPE_LINK
		networkSrc = *nwconfig.localAddr
	}

	newRoute := &netlink.Route{
		LinkIndex: nwconfig.link.Attrs().Index,
		Scope:     networkScope,
		Protocol:  networkProtocol,
		Dst: &net.IPNet{
			IP:   networkAddr,
			Mask: networkMask,
		},
		Src: networkSrc,
		Gw:  networkGateway,
	}

	routeStr = newRoute.Dst.String() + routeStr

	if err = networkLink.RouteAppend(newRoute); err == nil {
		klog.V(3).Infof("Configured route %s for interface '%s'",
			routeStr, nwconfig.link.Attrs().Name)
	} else {
		if errors.Is(err, os.ErrExist) {
			var noerr error
			err = noerr
			klog.V(3).Infof("Route %s already exists for interface '%s'",
				routeStr, nwconfig.link.Attrs().Name)
		} else {
			klog.Warningf("Could not add route %s for interface '%s': %v",
				routeStr, nwconfig.link.Attrs().Name, err)
		}
	}

	return err
}

func interfacesSetMTU(networkConfigurations map[string]*networkConfiguration, mtu int) {
	if mtu == 0 {
		return
	}

	for _, nwconfig := range networkConfigurations {
		if err := networkLink.LinkSetMTU(nwconfig.link, mtu); err != nil {
			klog.Warningf("Could not set MTU %d for interface '%s': %v",
				mtu, nwconfig.link.Attrs().Name, err)
		}
	}
}

func removeExistingIPs(networkConfigs map[string]*networkConfiguration) error {
	for _, nwconfig := range networkConfigs {
		addrs, err := networkLink.AddrList(nwconfig.link, netlink.FAMILY_V4)
		if err != nil {
			return err
		}

		for _, addr := range addrs {
			if err := networkLink.AddrDel(nwconfig.link, &addr); err != nil {
				return err
			}
		}
	}

	return nil
}

func configureInterfaces(networkConfigs map[string]*networkConfiguration) (int, int) {
	configured := 0

	klog.Infof("Configuring interfaces...")

	for _, nwconfig := range networkConfigs {
		if nwconfig.localAddr == nil {
			continue
		}

		addrs, err := networkLink.AddrList(nwconfig.link, netlink.FAMILY_V4)
		ifname := nwconfig.link.Attrs().Name
		if err != nil {
			klog.Warningf("Could not get addresses for link '%s': %v", ifname, err)
			continue
		}

		foundExisting := false

		for _, addr := range addrs {
			if nwconfig.localAddr.Equal(addr.IPNet.IP) {
				klog.Infof("Interface '%s' already configured with address %s",
					ifname, addr.IPNet.String())

				foundExisting = true

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
			// AddrAdd will add the corresponding /30 network route
			if err := networkLink.AddrAdd(nwconfig.link, newlinkaddr); err != nil {
				klog.Warningf("Could not configure address %s for interface '%s': %v",
					nwconfig.localAddr.String(), ifname, err)
				continue
			}

			klog.Infof("Configured address and route %s for interface '%s'",
				newlinkaddr.IPNet.String(), ifname)
		} else {
			// IP address exists, but we need to ensure the
			// existence of the corresponding /30 network route
			if err = addRoute(nwconfig, RouteMaskPointToPoint); err != nil {
				continue
			}
		}

		if err = addRoute(nwconfig, RouteMaskRoutedNetwork); err != nil {
			continue
		}

		configured++
	}

	return configured, len(networkConfigs)
}
