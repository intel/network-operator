/*
 * Copyright (C) 2026 Intel Corporation
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
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
)

// restoreGlobals saves the package level netlink and lldptool hooks and puts
// them back once the test is done, so that stubs do not leak between tests.
func restoreGlobals(t *testing.T) {
	t.Helper()

	origNetworkLink := networkLink
	origBinary := lldpBinary
	origPath := lldpPath

	t.Cleanup(func() {
		networkLink = origNetworkLink
		lldpBinary = origBinary
		lldpPath = origPath
	})
}

// fakeLinkSubscribeUp feeds one netlink update per fake interface with the link
// flagged up, which is what interfacesUp() waits for before continuing.
func fakeLinkSubscribeUp(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
	fakedata := getFakeNetworkData()
	links := make([]netlink.Link, 0, len(fakedata))

	for ifname, data := range fakedata {
		links = append(links, &fakeLink{
			fakeAttrs: netlink.LinkAttrs{
				Name:         ifname,
				HardwareAddr: data.nwconfig.link.Attrs().HardwareAddr,
				Flags:        net.FlagUp,
			},
		})
	}

	go func() {
		for _, link := range links {
			select {
			case ch <- netlink.LinkUpdate{Link: link}:
			case <-done:
				return
			}
		}
	}()

	return nil
}

// fakeLinkSubscribeOnce feeds the interface updates for the first subscription
// only and fails every subscription made after that one.
func fakeLinkSubscribeOnce() func(chan<- netlink.LinkUpdate, <-chan struct{}) error {
	subscriptions := 0

	return func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		subscriptions++
		if subscriptions > 1 {
			return fmt.Errorf("no more netlink subscriptions")
		}

		return fakeLinkSubscribeUp(ch, done)
	}
}

// l3NetworkConfigs returns the fake network configurations that have a peer
// address in their LLDP port description, ie. what the configurations look
// like after a successful LLDP discovery.
func l3NetworkConfigs() map[string]*networkConfiguration {
	netConfs := map[string]*networkConfiguration{}

	for ifname, fakedata := range getFakeNetworkData() {
		if !fakedata.shouldConfigure {
			continue
		}

		nwconfig := fakedata.nwconfig
		hwaddr := nwconfig.link.Attrs().HardwareAddr

		nwconfig.moduleId = fakedata.moduleid
		nwconfig.localHwAddr = &hwaddr

		netConfs[ifname] = &nwconfig
	}

	_ = lldpResults(netConfs)

	return netConfs
}

// fakeGaudiEnvironment creates a fake sysfs tree with the fake Gaudi devices in
// it and stubs out all netlink calls needed to run through cmdRun().
func fakeGaudiEnvironment(t *testing.T) {
	t.Helper()

	restoreGlobals(t)

	testSysfsRoot := t.TempDir()
	writeFakeSysfsEntries(testSysfsRoot, getFakeNetworkData(), t)
	t.Setenv("SYSFS_ROOT", testSysfsRoot)

	networkLink.LinkByName = fakeLinkByName
	networkLink.LinkSubscribe = fakeLinkSubscribeUp
	networkLink.LinkSetUp = func(link netlink.Link) error { return nil }
	networkLink.LinkSetDown = func(link netlink.Link) error { return nil }
	networkLink.LinkSetMTU = func(link netlink.Link, mtu int) error { return nil }
	networkLink.AddrList = fakeLinkAddrList
	networkLink.AddrAdd = fakeLinkAddrAdd
	networkLink.AddrDel = func(link netlink.Link, addr *netlink.Addr) error { return nil }
	networkLink.RouteAppend = fakeRouteAppend
}

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name     string
		config   cmdConfig
		success  bool
		wantMTU  int
		wantMode string
		wantPFC  string
	}{
		{"unset MTU is left alone", cmdConfig{mode: L3}, true, 0, L3, ""},
		{"too small MTU is raised", cmdConfig{mode: L3, mtu: 68}, true, 1500, L3, ""},
		{"1499 is raised to 1500", cmdConfig{mode: L3, mtu: 1499}, true, 1500, L3, ""},
		{"1500 is kept as is", cmdConfig{mode: L3, mtu: 1500}, true, 1500, L3, ""},
		{"9000 is kept as is", cmdConfig{mode: L3, mtu: 9000}, true, 9000, L3, ""},
		{"too large MTU is limited", cmdConfig{mode: L3, mtu: 9001}, true, 9000, L3, ""},
		{"lower case l3 is accepted", cmdConfig{mode: "l3"}, true, 0, L3, ""},
		{"lower case l2 is accepted", cmdConfig{mode: "l2"}, true, 0, L2, ""},
		{"upper case L2 is accepted", cmdConfig{mode: L2}, true, 0, L2, ""},
		{"empty mode is rejected", cmdConfig{}, false, 0, "", ""},
		{"unknown mode is rejected", cmdConfig{mode: "L4"}, false, 0, "", ""},
		{"PFC priorities are ordered", cmdConfig{mode: L3, pfc: "3,1"}, true, 0, L3, "1,3"},
		{"PFC 'none' disables PFC", cmdConfig{mode: L3, pfc: pfcDisable}, true, 0, L3, pfcDisable},
		{"out of range PFC is rejected", cmdConfig{mode: L3, pfc: "8"}, false, 0, L3, ""},
		{"non numeric PFC is rejected", cmdConfig{mode: L3, pfc: "high"}, false, 0, L3, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config

			err := sanitizeInput(&config)

			if !tt.success {
				if err == nil {
					t.Fatalf("expected %v to be rejected", tt.config)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error for %v: %v", tt.config, err)
			}
			if config.mtu != tt.wantMTU {
				t.Errorf("expected MTU %d, got %d", tt.wantMTU, config.mtu)
			}
			if config.mode != tt.wantMode {
				t.Errorf("expected mode '%s', got '%s'", tt.wantMode, config.mode)
			}
			if config.pfc != tt.wantPFC {
				t.Errorf("expected PFC '%s', got '%s'", tt.wantPFC, config.pfc)
			}
		})
	}
}

func TestSetupCmd(t *testing.T) {
	cmd, err := setupCmd()
	if err != nil {
		t.Fatalf("setupCmd failed: %v", err)
	}

	if cmd.Use != "discover" {
		t.Errorf("expected command 'discover', got '%s'", cmd.Use)
	}

	defaults := map[string]string{
		"mode":                   L3,
		"configure":              "false",
		"disable-networkmanager": "false",
		"interfaces":             "",
		"wait":                   "30s",
		"gaudinet":               "",
		"keep-running":           "false",
		"systemd-networkd":       "",
		"mtu":                    "0",
		"pfc":                    "",
		"metrics-bind-address":   "",
	}

	for name, want := range defaults {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("flag '%s' is not defined", name)

			continue
		}

		if flag.DefValue != want {
			t.Errorf("flag '%s' default is '%s', expected '%s'", name, flag.DefValue, want)
		}
	}

	// klog's own flags are added to the command as well
	if cmd.Flags().Lookup("v") == nil {
		t.Error("klog flags were not added to the command")
	}
}

func TestSetupCmdRun(t *testing.T) {
	restoreGlobals(t)

	cmd, err := setupCmd()
	if err != nil {
		t.Fatalf("setupCmd failed: %v", err)
	}

	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--mode=L4"})

	if err := cmd.Execute(); err == nil {
		t.Error("command should have failed for an invalid mode")
	}
}

func TestPreCleanups(t *testing.T) {
	if err := preCleanups(&cmdConfig{}); err != nil {
		t.Errorf("preCleanups without a systemd-networkd directory failed: %v", err)
	}

	testNetworkdDir, err := os.MkdirTemp("", "networkd")
	if err != nil {
		t.Errorf("cannot create tmp dir: %v", err)
	}
	defer os.RemoveAll(testNetworkdDir)

	networkd := filepath.Join(testNetworkdDir, "subdir")
	if err := preCleanups(&cmdConfig{networkd: networkd}); err != nil {
		t.Errorf("preCleanups failed: %v", err)
	}

	if stat, err := os.Stat(networkd); err != nil || !stat.IsDir() {
		t.Errorf("'%s' was not created as a directory: %v", networkd, err)
	}

	// a plain file in the path prevents the directory from being created
	blocker := filepath.Join(testNetworkdDir, "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("cannot create test file: %v", err)
	}

	if err := preCleanups(&cmdConfig{networkd: filepath.Join(blocker, "networkd")}); err == nil {
		t.Error("preCleanups should have failed for a directory below a file")
	}
}

func TestInitializeInterfaces(t *testing.T) {
	restoreGlobals(t)

	netConfs := getFakeNetworkDataConfigs()
	mtuSet := 0

	networkLink.LinkSubscribe = fakeLinkSubscribeUp
	networkLink.LinkSetUp = func(link netlink.Link) error { return nil }
	networkLink.LinkSetMTU = func(link netlink.Link, mtu int) error {
		mtuSet++

		return nil
	}
	networkLink.AddrList = fakeLinkAddrList
	networkLink.AddrDel = func(link netlink.Link, addr *netlink.Addr) error { return nil }

	if err := initializeInterfaces(&cmdConfig{mtu: 9000}, netConfs); err != nil {
		t.Errorf("initializeInterfaces failed: %v", err)
	}

	if mtuSet != len(netConfs) {
		t.Errorf("expected the MTU to be set for %d interfaces, got %d", len(netConfs), mtuSet)
	}

	// failing address removal is returned
	networkLink.AddrList = fakeLinkAddrListErr

	if err := initializeInterfaces(&cmdConfig{}, getFakeNetworkDataConfigs()); err == nil {
		t.Error("initializeInterfaces should have failed to remove existing addresses")
	}

	// failing netlink subscription is returned
	networkLink.LinkSubscribe = func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		return fmt.Errorf("no subscribing")
	}

	if err := initializeInterfaces(&cmdConfig{}, getFakeNetworkDataConfigs()); err == nil {
		t.Error("initializeInterfaces should have failed to set the interfaces up")
	}
}

func TestWriteL3Configuration(t *testing.T) {
	netConfs := l3NetworkConfigs()

	// nothing is written without a gaudinet file or a networkd directory
	if err := writeL3Configuration(&cmdConfig{}, netConfs); err != nil {
		t.Errorf("writeL3Configuration failed: %v", err)
	}

	dir := t.TempDir()
	gaudinet := filepath.Join(dir, "gaudinet.json")
	networkd := filepath.Join(dir, "networkd")

	if err := os.MkdirAll(networkd, 0755); err != nil {
		t.Fatalf("cannot create test directory: %v", err)
	}

	config := &cmdConfig{gaudinetfile: gaudinet, networkd: networkd}
	if err := writeL3Configuration(config, netConfs); err != nil {
		t.Errorf("writeL3Configuration failed: %v", err)
	}

	if _, err := os.Stat(gaudinet); err != nil {
		t.Errorf("gaudinet file was not written: %v", err)
	}

	for ifname := range netConfs {
		filename := networkdFilename(networkd, ifname)
		if _, err := os.Stat(filename); err != nil {
			t.Errorf("networkd file '%s' was not written: %v", filename, err)
		}
	}

	// a failing gaudinet write is only logged
	config = &cmdConfig{gaudinetfile: filepath.Join(dir, "missing", "gaudinet.json")}
	if err := writeL3Configuration(config, netConfs); err != nil {
		t.Errorf("a failing gaudinet write should not fail the configuration: %v", err)
	}

	// a failing networkd write is returned
	config = &cmdConfig{networkd: filepath.Join(dir, "missing")}
	if err := writeL3Configuration(config, netConfs); err == nil {
		t.Error("writeL3Configuration should have failed for a missing networkd directory")
	}
}

func TestPostCleanups(t *testing.T) {
	restoreGlobals(t)

	lldpBinary = lldpBinarySuccess
	if err := LookupLLDPTool(); err != nil {
		t.Fatalf("lldp binary '%s' not found: %v", lldpBinary, err)
	}

	netConfs := l3NetworkConfigs()
	downCount := 0

	networkLink.LinkSubscribe = fakeLinkSubscribeUp
	networkLink.LinkSetDown = func(link netlink.Link) error {
		downCount++

		return nil
	}
	networkLink.AddrList = fakeLinkAddrList
	networkLink.AddrDel = func(link netlink.Link, addr *netlink.Addr) error { return nil }

	// the interfaces were originally down and were set up by the discovery
	for _, nwconfig := range netConfs {
		nwconfig.link.Attrs().Flags |= net.FlagUp
	}

	postCleanups(&cmdConfig{pfc: "0,1"}, netConfs)

	if downCount != len(netConfs) {
		t.Errorf("expected %d interfaces to be set back down, got %d", len(netConfs), downCount)
	}
}

func TestPostCleanupsWarnings(t *testing.T) {
	restoreGlobals(t)

	lldpBinary = lldpBinaryFailure
	if err := LookupLLDPTool(); err != nil {
		t.Fatalf("lldp binary '%s' not found: %v", lldpBinary, err)
	}

	netConfs := l3NetworkConfigs()
	for _, nwconfig := range netConfs {
		nwconfig.link.Attrs().Flags |= net.FlagUp
	}

	networkLink.LinkSubscribe = func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		return fmt.Errorf("no subscribing")
	}
	networkLink.LinkSetDown = func(link netlink.Link) error { return fmt.Errorf("cannot set down") }
	networkLink.AddrList = fakeLinkAddrListErr

	// all failures during the cleanup are only logged
	postCleanups(&cmdConfig{pfc: "0,1"}, netConfs)
}

func TestDetectLLDP(t *testing.T) {
	netConfs := l3NetworkConfigs()
	config := &cmdConfig{ctx: context.Background(), timeout: 50 * time.Millisecond}

	portDescriptions := map[string]string{}
	for ifname, nwconfig := range netConfs {
		portDescriptions[ifname] = nwconfig.portDescription
	}

	// the links are all down, so no LLDP discovery is started at all
	detectLLDP(config, netConfs)

	for ifname, nwconfig := range netConfs {
		if nwconfig.portDescription != portDescriptions[ifname] {
			t.Errorf("port description of '%s' changed to '%s'", ifname, nwconfig.portDescription)
		}
	}

	// with the links up an LLDP client is started for each of them, but the
	// fake interfaces do not exist so every client fails immediately
	for _, nwconfig := range netConfs {
		nwconfig.link.Attrs().Flags |= net.FlagUp
	}

	detectLLDP(config, netConfs)

	for ifname, nwconfig := range netConfs {
		if nwconfig.portDescription != portDescriptions[ifname] {
			t.Errorf("port description of '%s' changed to '%s'", ifname, nwconfig.portDescription)
		}
	}
}

func TestCmdRunErrors(t *testing.T) {
	restoreGlobals(t)

	if err := cmdRun(&cmdConfig{ctx: context.Background(), mode: "L4"}); err == nil {
		t.Error("cmdRun should have failed for an invalid mode")
	}

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("cannot create test file: %v", err)
	}

	config := &cmdConfig{ctx: context.Background(), mode: L2, networkd: filepath.Join(blocker, "networkd")}
	if err := cmdRun(config); err == nil {
		t.Error("cmdRun should have failed to pre-cleanup")
	}

	lldpBinary = "no-such-lldptool-binary"
	if err := cmdRun(&cmdConfig{ctx: context.Background(), mode: L2, pfc: "1"}); err == nil {
		t.Error("cmdRun should have failed for a missing lldptool")
	}

	// an empty sysfs tree has no Gaudi network interfaces in it
	t.Setenv("SYSFS_ROOT", t.TempDir())

	if err := cmdRun(&cmdConfig{ctx: context.Background(), mode: L2}); err == nil {
		t.Error("cmdRun should have failed when no interfaces were found")
	}

	// additionally requested interfaces have to exist
	if err := cmdRun(&cmdConfig{ctx: context.Background(), mode: L2, ifaces: "no-such-iface"}); err == nil {
		t.Error("cmdRun should have failed for an unknown interface")
	}
}

func TestCmdRunInterfaceErrors(t *testing.T) {
	fakeGaudiEnvironment(t)

	networkLink.LinkSubscribe = func(ch chan<- netlink.LinkUpdate, done <-chan struct{}) error {
		return fmt.Errorf("no subscribing")
	}

	config := &cmdConfig{ctx: context.Background(), mode: L2}
	if err := cmdRun(config); err == nil {
		t.Error("cmdRun should have failed to initialize the interfaces")
	}

	// no LLDP peers are found, so the systemd-networkd configuration cannot
	// be written
	networkLink.LinkSubscribe = fakeLinkSubscribeUp

	config = &cmdConfig{
		ctx:      context.Background(),
		mode:     L3,
		timeout:  50 * time.Millisecond,
		networkd: filepath.Join(t.TempDir(), "networkd"),
	}
	if err := cmdRun(config); err == nil {
		t.Error("cmdRun should have failed to write the L3 configuration")
	}

	// the interfaces are restored back down when nothing is configured
	networkLink.LinkSubscribe = fakeLinkSubscribeOnce()

	config = &cmdConfig{ctx: context.Background(), mode: L2}
	if err := cmdRun(config); err == nil {
		t.Error("cmdRun should have failed to restore the interfaces")
	}
}

func TestCmdRunMetricsFailure(t *testing.T) {
	if stat, err := os.Stat(nfdFeatureDir); err == nil && stat.IsDir() {
		t.Skipf("'%s' exists on this host, not writing to it", nfdFeatureDir)
	}

	fakeGaudiEnvironment(t)

	// a metrics server that cannot be started stops the idling
	config := &cmdConfig{
		ctx:                context.Background(),
		mode:               L2,
		configure:          true,
		keepRunning:        true,
		metricsBindAddress: "127.0.0.1:-1",
	}

	if err := cmdRun(config); err == nil {
		t.Error("cmdRun should have failed for a failing metrics server")
	}
}

func TestMainCommand(t *testing.T) {
	restoreGlobals(t)

	origArgs := os.Args
	t.Cleanup(func() {
		os.Args = origArgs
	})

	// main() logs the failure instead of returning it
	os.Args = []string{"discover", "--mode=L4"}

	main()
}

func TestCmdRunL2(t *testing.T) {
	fakeGaudiEnvironment(t)

	lldpBinary = lldpBinarySuccess

	config := &cmdConfig{
		ctx:       context.Background(),
		mode:      "l2",
		configure: true,
		mtu:       9000,
		pfc:       "1,3",
		// an unusable port makes the metrics server fail without leaving
		// a listening socket behind for the rest of the test run
		metricsBindAddress: "127.0.0.1:-1",
	}

	if err := cmdRun(config); err != nil {
		t.Errorf("cmdRun failed: %v", err)
	}
}

func TestCmdRunL3(t *testing.T) {
	fakeGaudiEnvironment(t)

	dir := t.TempDir()
	config := &cmdConfig{
		ctx:          context.Background(),
		mode:         L3,
		timeout:      50 * time.Millisecond,
		gaudinetfile: filepath.Join(dir, "gaudinet.json"),
	}

	// no LLDP peers are found for the fake interfaces, so nothing gets
	// configured and the interfaces are restored back down
	if err := cmdRun(config); err != nil {
		t.Errorf("cmdRun failed: %v", err)
	}
}

func TestCmdRunPFCFailure(t *testing.T) {
	fakeGaudiEnvironment(t)

	lldpBinary = lldpBinaryFailure

	config := &cmdConfig{ctx: context.Background(), mode: L2, configure: true, pfc: "0"}

	if err := cmdRun(config); err == nil {
		t.Error("cmdRun should have failed to configure PFC")
	}
}

func TestCmdRunKeepRunning(t *testing.T) {
	if stat, err := os.Stat(nfdFeatureDir); err == nil && stat.IsDir() {
		t.Skipf("'%s' exists on this host, not writing to it", nfdFeatureDir)
	}

	fakeGaudiEnvironment(t)

	// Take SIGTERM over for the test binary before cmdRun() is started, so
	// that the signal used to stop it cannot terminate the test run itself.
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)

	defer signal.Stop(term)

	config := &cmdConfig{ctx: context.Background(), mode: L2, configure: true, keepRunning: true}
	result := make(chan error, 1)

	go func() {
		result <- cmdRun(config)
	}()

	// cmdRun() only reacts to a signal once it has installed its own handler,
	// so keep signalling until it returns.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	deadline := time.After(30 * time.Second)

	for {
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("cmdRun failed: %v", err)
			}

			return

		case <-ticker.C:
			if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
				t.Fatalf("cannot signal the test process: %v", err)
			}

		case <-deadline:
			t.Fatal("cmdRun did not return after SIGTERM")
		}
	}
}
