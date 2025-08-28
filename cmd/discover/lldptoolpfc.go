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
	"os/exec"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

const (
	LLDPToolBinary = "lldptool"

	pfcDisable = "none"
)

var (
	lldpPath   string
	lldpBinary string = LLDPToolBinary
)

func LookupLLDPTool() error {
	var err error

	lldpPath, err = exec.LookPath(lldpBinary)
	if err == nil {
		klog.Infof("lldptool '%s'", lldpPath)
	}

	return err
}

func VerifyPFCArgument(pfc string) (string, error) {
	strs := strings.Split(strings.TrimSpace(pfc), ",")

	if len(strs) == 1 && (strs[0] == "" || strs[0] == pfcDisable) {
		return "", nil
	}

	if len(strs) > 8 {
		return "", fmt.Errorf("%d PFC values, max is 8", len(strs))
	}

	result := [8]bool{}
	for _, s := range strs {
		t := strings.TrimSpace(s)
		i, err := strconv.Atoi(t)
		if err != nil {
			return "", fmt.Errorf("PFC value is not a number: %v", err)
		}
		if i < 0 || i > 7 {
			return "", fmt.Errorf("PFC value %d not in range 0-7", i)
		}
		result[i] = true
	}

	ordered := []string{}
	for i, v := range result {
		if v {
			ordered = append(ordered, strconv.Itoa(i))
		}
	}
	return strings.Join(ordered, ","), nil
}

func execPFC(ifname string, pfc string) error {
	var err error

	cmd := exec.Command(lldpPath, "-L", "-i", ifname, "adminStatus=rxtx")
	err = cmd.Run()
	if err == nil {
		klog.Infof("successfully run '%s'", strings.Join(cmd.Args, " "))
	} else {
		klog.Warningf("failed to run '%s': %v", strings.Join(cmd.Args, " "), err)
		return err
	}

	enabled := fmt.Sprintf("enabled=%s", pfc)
	cmd = exec.Command(lldpPath, "-T", "-i", ifname, "-V", "PFC",
		"enableTx=yes", enabled)
	err = cmd.Run()
	if err == nil {
		klog.Infof("successfully run '%s'", strings.Join(cmd.Args, " "))
	} else {
		klog.Warningf("failed to run '%s': %v", strings.Join(cmd.Args, " "), err)
		return err
	}

	return nil
}

func EnableAllPFC(pfc string, networkConfigs map[string]*networkConfiguration) error {
	for ifname := range networkConfigs {
		if err := execPFC(ifname, pfc); err != nil {
			return err
		}

		klog.V(3).Infof("Enabled PFCs '%s' for interface %s", pfc, ifname)
	}

	return nil
}

func DisableAllPFC(networkConfigs map[string]*networkConfiguration) error {
	for ifname := range networkConfigs {
		if err := execPFC(ifname, pfcDisable); err != nil {
			return err
		}

		klog.V(3).Infof("Disabled PFC for interface %s", ifname)
	}

	return nil
}
