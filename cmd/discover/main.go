/*
 * Copyright (C) 2024-2025 Intel Corporation
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
	goflag "flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/intel/network-operator/pkg/lldp"

	nm "github.com/intel/network-operator/internal/nm"
)

const (
	L2 = "L2"
	L3 = "L3"

	nfdFeatureDir         = "/etc/kubernetes/node-feature-discovery/features.d/"
	nfdLabelFile          = nfdFeatureDir + "scale-out-readiness.txt"
	nfdScaleOutReadyLabel = "intel.feature.node.kubernetes.io/gaudi-scale-out=true"
)

type cmdConfig struct {
	ctx          context.Context
	timeout      time.Duration
	configure    bool
	disableNM    bool
	gaudinetfile string
	ifaces       string
	mode         string
	keepRunning  bool
	networkd     string
	mtu          int
	pfc          string
}

func sanitizeInput(config *cmdConfig) error {
	if config.mtu > 0 && config.mtu < 1500 {
		klog.Infof("Forcing MTU value 1500 (old %d)", config.mtu)

		config.mtu = 1500
	} else if config.mtu > 9000 {
		klog.Infof("Limiting MTU value 9000 (old %d)", config.mtu)

		config.mtu = 9000
	}

	switch strings.ToUpper(config.mode) {
	case L3:
		config.mode = L3
	case L2:
		config.mode = L2
	default:
		return fmt.Errorf("Invalid mode '%s'", config.mode)
	}

	var err error
	config.pfc, err = VerifyPFCArgument(config.pfc)
	if err != nil {
		return fmt.Errorf("Invalid PFC configuration: %v", err)
	}

	return nil
}

func detectLLDP(config *cmdConfig, networkConfigs map[string]*networkConfiguration) {
	var wg sync.WaitGroup
	lldpResultChan := make(chan lldp.DiscoveryResult, len(networkConfigs))
	timeoutctx, cancelctx := context.WithTimeout(config.ctx, config.timeout)

	defer cancelctx()

	for _, networkconfig := range networkConfigs {
		if networkconfig.link.Attrs().Flags&net.FlagUp == 0 {
			klog.Infof("Link '%s' %s, cannot start LLDP\n",
				networkconfig.link.Attrs().Name, networkconfig.link.Attrs().OperState.String())
			continue
		}

		wg.Add(1)
		go func() {
			lldpClient := lldp.NewClient(timeoutctx, networkconfig.link.Attrs().Name, *networkconfig.localHwAddr)
			if err := lldpClient.Start(lldpResultChan); err != nil {
				klog.Infof("Cannot start LLDP client: %v\n", err)
			}
			wg.Done()
		}()

		klog.Infof("Started LLDP discovery for '%s'...\n", networkconfig.link.Attrs().Name)
	}

	wg.Wait()

	for len(lldpResultChan) > 0 {
		result := <-lldpResultChan

		if nwconfig, exists := networkConfigs[result.InterfaceName]; exists {
			nwconfig.portDescription = result.PortDescription

			var hwaddr net.HardwareAddr = result.PeerMAC
			nwconfig.peerHWAddr = &hwaddr
		}
	}
}

func preCleanups(config *cmdConfig) error {
	if _, err := os.Stat(nfdLabelFile); err == nil {
		klog.Infof("NFD label file already exists, removing it...\n")

		if err = os.Remove(nfdLabelFile); err != nil {
			klog.Warningf("Failed to remove NFD label file: %+v\n", err)
		}
	}

	if config.networkd != "" {
		if err := os.MkdirAll(config.networkd, 0744); err != nil {
			return fmt.Errorf("Cannot create systemd-networkd directory: %v", err)
		}
		klog.Infof("Created systemd-networkd directory %s", config.networkd)
	}

	return nil
}

func initializeInterfaces(config *cmdConfig, networkConfigs map[string]*networkConfiguration) error {
	if err := interfacesUp(networkConfigs); err != nil {
		return err
	}

	interfacesSetMTU(networkConfigs, config.mtu)

	if err := removeExistingIPs(networkConfigs); err != nil {
		return fmt.Errorf("Failed to remove any existing IPs from interfaces: %+v", err)
	}

	return nil
}

func writeL3Configuration(config *cmdConfig, networkConfigs map[string]*networkConfiguration) error {
	if config.gaudinetfile != "" {
		if err := WriteGaudiNet(config.gaudinetfile, networkConfigs); err != nil {
			klog.Errorf("Error: %v\n", err)
		}
	}

	if config.networkd != "" {
		if _, err := WriteSystemdNetworkd(config.networkd, networkConfigs); err != nil {
			return fmt.Errorf("Could not create systemd-networkd configuration files: %v\n", err)
		}
	}

	return nil
}

func postCleanups(config *cmdConfig, networkConfigs map[string]*networkConfiguration) {
	klog.Info("Clean up before exiting...")

	err := os.Remove(nfdLabelFile)
	if err != nil {
		klog.Warningf("Failed to remove NFD label file: %+v\n", err)
	}

	if config.pfc != "" {
		if err := DisableAllPFC(networkConfigs); err != nil {
			klog.Warningf("Failed to disable LLDP PFC on all interfaces: %v", err)
		}
	}

	klog.Infof("Restoring interfaces to original state...")
	if err := removeExistingIPs(networkConfigs); err != nil {
		klog.Warningf("Failed to remove any existing IPs from interfaces: %+v\n", err)
	}

	if err := interfacesRestoreDown(networkConfigs); err != nil {
		klog.Warningf("Failed to restore interfaces to original state: %+v\n", err)
	}
}

func cmdRun(config *cmdConfig) error {
	err := sanitizeInput(config)
	if err != nil {
		return err
	}

	if err := preCleanups(config); err != nil {
		return fmt.Errorf("Failed to pre-cleanup: %v", err)
	}

	if config.pfc != "" {
		if err := LookupLLDPTool(); err != nil {
			return fmt.Errorf("Could not find lldptool: %v", err)
		}
	}

	allInterfaces := getNetworks()

	if len(config.ifaces) > 0 {
		allInterfaces = append(allInterfaces, strings.Split(config.ifaces, ",")...)
	}

	if len(allInterfaces) == 0 {
		return fmt.Errorf("No interfaces found")
	}

	networkConfigs := getNetworkConfigs(allInterfaces)
	if len(networkConfigs) < len(allInterfaces) {
		return fmt.Errorf("Not all interfaces were found in the system")
	}

	if config.disableNM {
		nmapi, err := nm.NewNetworkManager()
		if err != nil {
			return fmt.Errorf("Failed to create NetworkManager: %v", err)
		}

		err = nm.DisableNetworkManagerForInterfaces(nmapi, allInterfaces)
		if err != nil {
			return fmt.Errorf("Failed to disable interfaces in NetworkManager: %v", err)
		}
	}

	if err := initializeInterfaces(config, networkConfigs); err != nil {
		return err
	}

	if config.mode == L3 {
		detectLLDP(config, networkConfigs)
		foundpeers := lldpResults(networkConfigs)

		if config.configure && foundpeers {
			numConfigured, numTotal := configureInterfaces(networkConfigs)
			if numConfigured < numTotal {
				return fmt.Errorf("Not all interfaces were configured (%d/%d).", numConfigured, numTotal)
			}
			klog.Infof("Configured %d of %d interfaces\n", numConfigured, numTotal)
		}

		if err := writeL3Configuration(config, networkConfigs); err != nil {
			return err
		}
	}

	logResults(config, networkConfigs)

	if !config.configure {
		if err := interfacesRestoreDown(networkConfigs); err != nil {
			return err
		}

		return nil
	}

	if config.pfc != "" {
		var err error

		if config.pfc == pfcDisable {
			err = DisableAllPFC(networkConfigs)
		} else {
			err = EnableAllPFC(config.pfc, networkConfigs)
		}

		if err != nil {
			return fmt.Errorf("Failed to configure PFC: %v", err)
		}
	}

	if config.keepRunning {
		if s, err := os.Stat(nfdFeatureDir); err == nil && s.IsDir() {
			content := nfdScaleOutReadyLabel + "\n"

			if err := os.WriteFile(nfdLabelFile, []byte(content), 0644); err != nil {
				return fmt.Errorf("Failed to write NFD label to indicate scale-out readiness: %+v\n", err)
			}
		}

		klog.Infof("Configurations done. Idling...")

		defer postCleanups(config, networkConfigs)

		term := make(chan os.Signal, 1)

		signal.Notify(term, os.Interrupt, syscall.SIGTERM)
		<-term
	}

	return nil
}

// error is always nil, but keep the logic incase we want to return it later on.
// nolint: unparam
func setupCmd() (*cobra.Command, error) {
	config := &cmdConfig{ctx: context.Background()}

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover and optionally configure network devices",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			return cmdRun(config)
		},
	}
	fs := goflag.FlagSet{}
	klog.InitFlags(&fs)

	cmd.Flags().AddGoFlagSet(&fs)
	cmd.Flags().SortFlags = false

	cmd.Flags().StringVarP(&config.mode, "mode", "", L3,
		"'L2' for network layer 2 or 'L3' for network layer 3 (L3) using LLDP")
	cmd.Flags().BoolVarP(&config.configure, "configure", "", false,
		"Configure L3 network with LLDP or set interfaces up with L2 networks")
	cmd.Flags().BoolVarP(&config.disableNM, "disable-networkmanager", "", false,
		"Disable Host's NetworkManager for interfaces")
	cmd.Flags().StringVarP(&config.ifaces, "interfaces", "", "",
		"Comma separated list of additional network interfaces")
	cmd.Flags().DurationVarP(&config.timeout, "wait", "", time.Second*30,
		"Time to wait for LLDP packets")
	cmd.Flags().StringVarP(&config.gaudinetfile, "gaudinet", "", "",
		"gaudinet file path")
	cmd.Flags().BoolVarP(&config.keepRunning, "keep-running", "", false,
		"Keep running after any configurations are done")
	cmd.Flags().StringVarP(&config.networkd, "systemd-networkd", "", "",
		"Write systemd networkd configuration files to given directory")
	cmd.Flags().IntVarP(&config.mtu, "mtu", "", 0,
		"MTU value to set for interfaces")
	cmd.Flags().StringVarP(&config.pfc, "pfc", "", "",
		"Comma separated list of Priority Flow Control priorities (0-7) to enable")

	return cmd, nil
}

func main() {
	defer klog.Flush()
	cmd, err := setupCmd()

	if err != nil {
		klog.Errorf("Could not start: %v\n", err)
		return
	}

	if err := cmd.Execute(); err != nil {
		klog.Errorf("Command execution failed: %v\n", err)
	}
}
