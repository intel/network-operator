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
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/vishvananda/netlink"
)

const (
	sysfsDevicePath = "bus/pci/devices"
	netDevicePath   = "net"
)

func TestSelectMask30L3Address(t *testing.T) {
	expectedpeer := net.IPv4(10, 210, 8, 122)
	expectedaddr := net.IPv4(10, 210, 8, 121)

	nwconfig := networkConfiguration{
		link: &fakeLink{
			fakeAttrs: netlink.LinkAttrs{
				Name: "eth_a",
			},
		},
		portDescription: "no-alert " + expectedpeer.String() + "/30",
	}

	peeraddr, localaddr, err := selectMask30L3Address(&nwconfig)
	if !peeraddr.Equal(expectedpeer) {
		t.Errorf("Peer addresses do not match, expected %s got %s: %v", expectedpeer.String(), peeraddr.String(), err)
	}
	if !localaddr.Equal(expectedaddr) {
		t.Errorf("Local addresses do not match, expected %s got %s: %v", expectedaddr.String(), localaddr.String(), err)
	}

	addrmask := "/16"
	addrtext := "10.210.8.122"
	nwconfig = networkConfiguration{
		link: &fakeLink{
			fakeAttrs: netlink.LinkAttrs{
				Name: "eth_a",
			},
		},
		portDescription: "no-alert " + addrtext + addrmask,
	}
	peeraddr, localaddr, err = selectMask30L3Address(&nwconfig)
	if err == nil || peeraddr.String() != addrtext || localaddr.String() != "<nil>" {
		t.Errorf("netmask %s unexpectedly returned values '%s', '%s' or no error '%v'",
			addrmask, peeraddr.String(), localaddr.String(), err)
	}
}

func TestSysFsRoot(t *testing.T) {
	testSysfsRoot, err := os.MkdirTemp("", "networkoperator.")
	if err != nil {
		t.Errorf("cannot create tmp dir: %v", err)
	}
	defer os.RemoveAll(testSysfsRoot)

	os.Setenv("SYSFS_ROOT", testSysfsRoot)
	defer os.Unsetenv("SYSFS_ROOT")

	detectedSysfsRoot := getSysfsRoot()
	if detectedSysfsRoot != testSysfsRoot {
		t.Errorf("Sysfs root directory is '%s', expected '%s'", detectedSysfsRoot, testSysfsRoot)
	}

	expectedpath := path.Join(testSysfsRoot, "bus/pci/drivers/habanalabs")
	if detectedsysfsdriverpath := sysfsDriverPath(); detectedsysfsdriverpath != expectedpath {
		t.Errorf("got sysfs driver path '%s', expected '%s'", detectedsysfsdriverpath, expectedpath)
	}

}

func writeFakeSysfsEntries(testSysfsRoot string, devices map[string]fakeNetworkTestData, t *testing.T) {
	driverdir := path.Join(testSysfsRoot, driverPath)
	if err := os.MkdirAll(driverdir, 0755); err != nil {
		t.Errorf("cannot create fake driver dir '%s': %v", driverdir, err)
	}

	pcidevicedir := path.Join(testSysfsRoot, sysfsDevicePath)

	for netdev, fakenwconfig := range devices {
		pcidev := fakenwconfig.pcidevice
		netdevice := path.Join(pcidevicedir, pcidev, netDevicePath, netdev)
		if err := os.MkdirAll(netdevice, 0755); err != nil {
			t.Errorf("cannot create fake PCI device dir '%s': %v", netdevice, err)
		}

		// ...bus/pci/drivers/habanalabs/xxxx:xx:xx.x -> ...bus/pci/devices/xxxx:xx:xx.x
		driverdirsymlink := path.Join(driverdir, pcidev)
		pcidirdevice := path.Join(pcidevicedir, pcidev)
		if err := os.Symlink(pcidirdevice, driverdirsymlink); err != nil {
			t.Errorf("cannot create symlink '%s' to '%s': %v", driverdirsymlink, pcidirdevice, err)
		}
	}

	acceldir := path.Join(testSysfsRoot, accelDevicePath)
	if err := os.MkdirAll(acceldir, 0755); err != nil {
		t.Errorf("cannot create fake accel dir '%s': %v", acceldir, err)
	}

	accelid := 8
	for _, fakenwconfig := range devices {
		acceldevicedir := filepath.Join(acceldir, fmt.Sprintf("accel%d", accelid), accelDeviceDir)
		if err := os.MkdirAll(acceldevicedir, 0755); err != nil {
			t.Errorf("cannot create fake accel device dir '%s': %v", acceldevicedir, err)
		}

		filename := filepath.Join(acceldevicedir, accelModuleIdFile)
		if err := os.WriteFile(filename, []byte(fakenwconfig.moduleid), 0644); err != nil {
			t.Errorf("cannot create fake moduleid file '%s': %v", filename, err)
		}

		filename = filepath.Join(acceldevicedir, accelPCIDeviceFile)
		if err := os.WriteFile(filename, []byte(fakenwconfig.pcidevice), 0644); err != nil {
			t.Errorf("cannot create fake PCI device file '%s': %v", filename, err)
		}

		accelid++
	}
}

type fakeLink struct {
	fakeAttrs netlink.LinkAttrs
}

func (l *fakeLink) Attrs() *netlink.LinkAttrs {
	return &l.fakeAttrs
}

func (l *fakeLink) Type() string {
	return ""
}

type fakeNetworkTestData struct {
	pcidevice       string
	moduleid        string
	linkaddrs       []net.IPNet
	nwconfig        networkConfiguration
	numIPAddrs      int
	shouldConfigure bool
}

func getFakeNetworkData() map[string]fakeNetworkTestData {
	return map[string]fakeNetworkTestData{
		// Address and proper LLDP Port Description field
		"eth_a": {
			pcidevice: "0000:aa:00.0",
			moduleid:  "0",
			linkaddrs: []net.IPNet{
				{
					IP:   net.IPv4(192, 192, 192, 1),
					Mask: net.IPv4Mask(255, 255, 255, 0),
				},
			},
			nwconfig: networkConfiguration{
				link: &fakeLink{
					fakeAttrs: netlink.LinkAttrs{
						Name:         "eth_a",
						HardwareAddr: net.HardwareAddr{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
					},
				},
				portDescription: "no-alert 10.210.8.122/30",
				peerHWAddr:      &net.HardwareAddr{0x01, 0x01, 0x02, 0x02, 0x03, 0x03},
			},
			numIPAddrs:      1,
			shouldConfigure: true,
		},
		// No address, Port Description field with other string
		"eth_b": {
			pcidevice: "0000:bb:00.0",
			moduleid:  "42",
			linkaddrs: []net.IPNet{},
			nwconfig: networkConfiguration{
				link: &fakeLink{
					fakeAttrs: netlink.LinkAttrs{
						Name:         "eth_b",
						HardwareAddr: net.HardwareAddr{0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x0a},
					},
				},
				portDescription: "unexpected port description",
				peerHWAddr:      &net.HardwareAddr{0x02, 0x02, 0x03, 0x03, 0x4, 0x4},
			},
			numIPAddrs:      0,
			shouldConfigure: false,
		},
		// Already configured with LLDP address 10.210.8.125/30
		"eth_c": {
			pcidevice: "0000:cc:00.0",
			moduleid:  "80",
			linkaddrs: []net.IPNet{
				{
					IP:   net.IPv4(10, 210, 8, 125),
					Mask: net.IPv4Mask(255, 255, 255, 252),
				},
			},
			nwconfig: networkConfiguration{
				link: &fakeLink{
					fakeAttrs: netlink.LinkAttrs{
						Name:         "eth_c",
						HardwareAddr: net.HardwareAddr{0x0c, 0x0d, 0x0e, 0x0f, 0x0a, 0x0b},
					},
				},
				portDescription: "no-alert 10.210.8.126/30",
				peerHWAddr:      &net.HardwareAddr{0x03, 0x03, 0x4, 0x4, 0x5, 0x5},
			},
			numIPAddrs:      0,
			shouldConfigure: true,
		},
	}
}

func getFakeNetworkDataConfigs() map[string]*networkConfiguration {
	nwconfigs := make(map[string]*networkConfiguration)
	for idx, fakenwconfigdata := range getFakeNetworkData() {
		nwconfigs[idx] = &fakenwconfigdata.nwconfig
	}
	return nwconfigs
}

func fakeLinkByName(name string) (netlink.Link, error) {
	fakenwconfigs := getFakeNetworkDataConfigs()
	if l := fakenwconfigs[name]; l != nil {
		return &fakeLink{
			fakeAttrs: netlink.LinkAttrs{
				Name:         l.link.Attrs().Name,
				HardwareAddr: l.link.Attrs().HardwareAddr,
			},
		}, nil
	}

	return nil, fmt.Errorf("no fake link for '%s' defined in test cases", name)
}

func TestFakeSysfs(t *testing.T) {
	networkLink.LinkByName = fakeLinkByName

	testSysfsRoot, err := os.MkdirTemp("", "networkoperator.")
	if err != nil {
		t.Errorf("cannot create tmp dir: %v", err)
	}
	defer os.RemoveAll(testSysfsRoot)

	os.Setenv("SYSFS_ROOT", testSysfsRoot)
	defer os.Unsetenv("SYSFS_ROOT")

	allInterfaces, nwConfigs, err := getNetworkConfigs([]string{})
	// no devices in the fake sysfs directory
	if len(allInterfaces) > 0 || len(nwConfigs) > 0 || err != nil {
		t.Errorf("no devices should have been found: %v, %v: %v", allInterfaces, nwConfigs, err)
	}

	devices := getFakeNetworkData()
	writeFakeSysfsEntries(testSysfsRoot, devices, t)

	_, nwConfigs, err = getNetworkConfigs([]string{})

	if err != nil {
		t.Errorf("network config returned unexpected error: %v", err)
	}

	for d, nw := range nwConfigs {
		if _, exists := devices[d]; !exists {
			t.Errorf("found unexpected device '%s' %v", d, nw)
		}
		delete(devices, d)
	}
	if len(devices) > 0 {
		t.Errorf("not all devices were detected: %v", devices)
	}
}

func TestLldpResults(t *testing.T) {
	nwconfigs := getFakeNetworkDataConfigs()
	foundpeers := lldpResults(nwconfigs)

	if !foundpeers {
		t.Errorf("expected to find at least one peer addresses, none found")
	}

	delete(nwconfigs, "eth_c")
	foundpeers = lldpResults(nwconfigs)
	if !foundpeers {
		t.Errorf("expected to find at least one peer addresses, none found")
	}

	delete(nwconfigs, "eth_a")
	foundpeers = lldpResults(nwconfigs)
	if foundpeers {
		t.Errorf("expected not to find any peer addresses, at least none found")
	}
}

func TestGetNetworkConfigs(t *testing.T) {
	networks := []string{"eth_a", "eth_b", "eth_c"}

	networkLink.LinkByName = fakeLinkByName

	testSysfsRoot, err := os.MkdirTemp("", "networkoperator.")
	if err != nil {
		t.Errorf("cannot create tmp dir: %v", err)
	}
	defer os.RemoveAll(testSysfsRoot)

	os.Setenv("SYSFS_ROOT", testSysfsRoot)
	defer os.Unsetenv("SYSFS_ROOT")

	writeFakeSysfsEntries(testSysfsRoot, getFakeNetworkData(), t)

	networknames, networkconfigs, err := getNetworkConfigs([]string{})

	if err != nil {
		t.Errorf("Error '%v' returned, expected none", err)
	}
	if len(networknames) != len(networkconfigs) {
		t.Errorf("%d network interface names returned, but %d networkConfigs returned",
			len(networknames), len(networkconfigs))
	}
	if len(networkconfigs) != len(networks) {
		t.Errorf("number of networkconfigs %d and networks %d don't match", len(networkconfigs), len(networks))
	}
	for _, iface := range networks {
		if _, exists := networkconfigs[iface]; !exists {
			t.Errorf("name '%s' was not found in networkconfigs", iface)
		}
		delete(networkconfigs, iface)
	}
	if len(networkconfigs) > 0 {
		t.Errorf("not all networkconfigs were created")
	}

	networks = []string{"eth_c", "eth_b", "foo"}
	networknames, networkconfigs, err = getNetworkConfigs(networks)

	if err == nil {
		t.Errorf("Expected error to be returned")
	}
	if len(networknames) != 0 {
		t.Errorf("wrong number (%d) of network names returned", len(networknames))
	}
	if len(networkconfigs) != 0 {
		t.Errorf("wrong number (%d) of networkconfigs returned", len(networkconfigs))
	}
	if _, exists := networkconfigs["foo"]; exists {
		t.Errorf("name 'foo' exists when it should not")
	}
	for _, iface := range networks {
		delete(networkconfigs, iface)
	}
	if len(networkconfigs) > 0 {
		t.Errorf("networkconfig has left over items")
	}

	_, networkconfigs, err = getNetworkConfigs([]string{})

	if err != nil {
		t.Errorf("received error %v, none expected", err)
	}

	fakenwconfigs := getFakeNetworkData()
	for ifname, nwconfig := range networkconfigs {
		if nwconfig.moduleId != fakenwconfigs[ifname].moduleid {
			t.Errorf("expected interface %s module id %s, got %s",
				ifname, fakenwconfigs[ifname].moduleid, nwconfig.moduleId)
		}
	}
}

func fakeLinkAddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	netlinkaddrs := []netlink.Addr{}
	name := link.Attrs().Name

	fakenwdata, exists := getFakeNetworkData()[name]
	if !exists {
		return nil, fmt.Errorf("fake link '%s' does not exist in test data for fakeLinkAddrList", name)
	}

	for _, linkaddr := range fakenwdata.linkaddrs {
		netlinkaddrs = append(netlinkaddrs, netlink.Addr{
			IPNet: &net.IPNet{
				IP:   linkaddr.IP,
				Mask: linkaddr.Mask,
			},
			Peer:      &net.IPNet{},
			Broadcast: net.IP{},
		})
	}

	return netlinkaddrs, nil
}

func fakeLinkAddrListErr(link netlink.Link, family int) ([]netlink.Addr, error) {
	return nil, fmt.Errorf("I'm broken")
}

func fakeLinkAddrAddErr(link netlink.Link, addr *netlink.Addr) error {
	return fmt.Errorf("I'm broken")
}

func fakeRouteAppend(route *netlink.Route) error {
	return nil
}

var fakeAddrsAdded []*netlink.Addr

func fakeLinkAddrAdd(link netlink.Link, addr *netlink.Addr) error {
	name := link.Attrs().Name

	if addr == nil {
		return fmt.Errorf("no address for fakeLinkAddrAdd interface '%s'", name)
	}

	if _, exists := getFakeNetworkData()[name]; !exists {
		return fmt.Errorf("fake link '%s' does not exist in test data for fakeLinkAddrAdd", name)
	}

	fakeAddrsAdded = append(fakeAddrsAdded, addr)

	return nil
}

func TestConfigureInterfaces(t *testing.T) {
	networkLink.AddrList = fakeLinkAddrList
	networkLink.AddrAdd = fakeLinkAddrAdd
	networkLink.RouteAppend = fakeRouteAppend

	fakeNetworkData := getFakeNetworkData()

	for iface, fnc := range fakeNetworkData {
		ifs := map[string]*networkConfiguration{
			iface: &fnc.nwconfig,
		}

		_ = lldpResults(ifs)

		// Modified by fakeLinkAddrAdd
		fakeAddrsAdded = []*netlink.Addr{}

		configured, _ := configureInterfaces(ifs)

		if fnc.shouldConfigure && configured == 0 {
			t.Error("interface did not configure when it should have", iface)
		}
		if !fnc.shouldConfigure && configured >= 1 {
			t.Error("interface configured when it shouldn't have", iface)
		}

		if fnc.numIPAddrs != len(fakeAddrsAdded) {
			t.Error("invalid amount of addresses added", iface, fnc.numIPAddrs, len(fakeAddrsAdded))
		}
	}
}

func TestConfigureInterfacesErrors(t *testing.T) {
	ifName := "eth_a"
	fnd := getFakeNetworkData()[ifName]

	ifs := map[string]*networkConfiguration{
		ifName: &fnd.nwconfig,
	}

	_ = lldpResults(ifs)

	networkLink.AddrList = fakeLinkAddrListErr

	configured, _ := configureInterfaces(ifs)

	if configured != 0 {
		t.Error("configure succeeded when error should have been returned")
	}

	networkLink.AddrList = fakeLinkAddrList
	networkLink.AddrAdd = fakeLinkAddrAddErr

	configured, _ = configureInterfaces(ifs)

	if configured != 0 {
		t.Error("configure succeeded when error should have been returned")
	}
}

func TestAddRouteErrors(t *testing.T) {
	ifName := "eth_a"
	fnd := getFakeNetworkData()[ifName]

	networkLink.AddrList = fakeLinkAddrList
	networkLink.AddrAdd = fakeLinkAddrAdd
	networkLink.RouteAppend = func(route *netlink.Route) error {
		return fmt.Errorf("oops..")
	}

	ip, _, _ := net.ParseCIDR("10.0.0.1/24")

	fnd.nwconfig.localAddr = &ip

	err := addRoute(&fnd.nwconfig, RouteMaskPointToPoint)

	if err == nil {
		t.Error("add route succeeded while it shouldn't have")
	}

	networkLink.RouteAppend = func(route *netlink.Route) error {
		return os.ErrExist
	}

	err = addRoute(&fnd.nwconfig, RouteMaskPointToPoint)

	if err != nil {
		t.Error("add route failed while it shouldn't have")
	}

	fnd.nwconfig.localAddr = nil

	err = addRoute(&fnd.nwconfig, RouteMaskPointToPoint)

	if err == nil {
		t.Error("add route succeeded while it shouldn't have")
	}
}

func TestNoGaudiDevicesErrors(t *testing.T) {
	networkLink.LinkByName = fakeLinkByName

	// invalid directory to make Glob fail
	os.Setenv("SYSFS_ROOT", "\\\\\\")
	defer os.Unsetenv("SYSFS_ROOT")

	allinterfaces, nwconfigs, _ := getNetworkConfigs([]string{})
	if len(allinterfaces) > 0 || len(nwconfigs) > 0 {
		t.Errorf("no devices should have been found: %s", allinterfaces)
	}
}

func TestLogResults(t *testing.T) {
	cmd := &cmdConfig{
		ctx:          context.Background(),
		gaudinetfile: "gaudinet.json",
		ifaces:       "eth_a,eth_b,eth_c",
		mode:         L2,
	}

	netConfs := getFakeNetworkDataConfigs()

	logResults(cmd, netConfs)

	cmd.mode = L3

	logResults(cmd, netConfs)

	t.Log("LogResults is just executed")
}

func TestAllLinksResponded(t *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	for _, nwconfig := range netConfs {
		nwconfig.expectResponse = true
		break
	}

	if allLinksResponded(netConfs) {
		t.Error("expected all links not to respond")
	}

	for _, nwconfig := range netConfs {
		nwconfig.expectResponse = false
	}

	if !allLinksResponded(netConfs) {
		t.Error("expected all links to respond")
	}
}

func TestInterfaceUp(t *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	networkLink.LinkSubscribe = func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		return nil
	}

	linkUpCount := 0
	networkLink.LinkSetUp = func(link netlink.Link) error {
		linkUpCount++
		return nil
	}

	// We ignore the error as we have no control for the link update mechanism.
	_ = interfacesUp(netConfs)

	if linkUpCount != len(netConfs) {
		t.Errorf("interfacesUp should have set %d links up, but set %d", len(netConfs), linkUpCount)
	}
}

func TestInterfaceUpErrors(t *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	networkLink.LinkSubscribe = func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		return fmt.Errorf("error subscribing")
	}

	err := interfacesUp(netConfs)
	if err == nil {
		t.Error("interfacesUp should have failed")
	}

	networkLink.LinkSubscribe = netlink.LinkSubscribe
	networkLink.LinkSetUp = func(link netlink.Link) error {
		return fmt.Errorf("error link set up")
	}

	err = interfacesUp(netConfs)
	if err != nil {
		t.Error("interfacesUp should have succeeded")
	}
}

func TestInterfaceDown(t *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	networkLink.LinkSubscribe = func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		return nil
	}
	networkLink.LinkSetDown = func(link netlink.Link) error {
		return nil
	}

	for _, nwconfig := range netConfs {
		nwconfig.link.Attrs().Flags ^= net.FlagUp
	}

	err := interfacesRestoreDown(netConfs)
	if err != nil {
		t.Error("interfacesRestoreDown should have succeeded")
	}
}

func TestInterfaceDownErrors(t *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	networkLink.LinkSubscribe = func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		return fmt.Errorf("no subscribing")
	}
	networkLink.LinkSetDown = func(link netlink.Link) error {
		return fmt.Errorf("cant set down")
	}

	for _, nwconfig := range netConfs {
		nwconfig.link.Attrs().Flags ^= net.FlagUp
	}

	err := interfacesRestoreDown(netConfs)
	if err == nil {
		t.Error("interfacesRestoreDown should have failed")
	}
}

func TestSetLinkMTUWarning(_ *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	networkLink.LinkSetMTU = func(link netlink.Link, mtu int) error {
		return fmt.Errorf("cant set mtu")
	}

	interfacesSetMTU(netConfs, 8080)
}

func TestRemoveExistingIPs(t *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	networkLink.AddrDel = func(link netlink.Link, addr *netlink.Addr) error {
		return nil
	}
	networkLink.AddrList = fakeLinkAddrList

	err := removeExistingIPs(netConfs)
	if err != nil {
		t.Error("removeExistingIPs should have passed")
	}
}

func TestRemoveExistingIPsErrors(t *testing.T) {
	netConfs := getFakeNetworkDataConfigs()

	networkLink.AddrList = fakeLinkAddrListErr

	err := removeExistingIPs(netConfs)
	if err == nil {
		t.Error("removeExistingIPs should have failed")
	}

	networkLink.AddrList = fakeLinkAddrList

	networkLink.AddrDel = func(link netlink.Link, addr *netlink.Addr) error {
		return fmt.Errorf("cant remove addr")
	}

	err = removeExistingIPs(netConfs)
	if err == nil {
		t.Error("removeExistingIPs should have failed")
	}
}
