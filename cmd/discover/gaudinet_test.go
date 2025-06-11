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
	"testing"

	"github.com/vishvananda/netlink"
)

func fakenetworkconfigs() (map[string]*networkConfiguration, string) {
	lldpPeer := net.IPv4(10, 120, 0, 2)
	localAddr := net.IPv4(10, 120, 0, 1)
	networkconfig := networkConfiguration{
		link: &fakeLink{
			fakeAttrs: netlink.LinkAttrs{
				HardwareAddr: net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			},
		},
		lldpPeer:   &lldpPeer,
		localAddr:  &localAddr,
		peerHWAddr: &net.HardwareAddr{0x06, 0x05, 0x04, 0x03, 0x02, 0x01},
	}
	expectedoutput := "{\n" +
		"  \"NIC_NET_CONFIG\": [\n" +
		"    {\n" +
		"      \"NIC_MAC\": \"01:02:03:04:05:06\",\n" +
		"      \"NIC_IP\": \"10.120.0.1\",\n" +
		"      \"SUBNET_MASK\": \"255.255.255.252\",\n" +
		"      \"GATEWAY_MAC\": \"06:05:04:03:02:01\"\n" +
		"    }\n" +
		"  ]\n" +
		"}\n"

	nwconfigs := make(map[string]*networkConfiguration)
	nwconfigs["eth1234"] = &networkconfig

	return nwconfigs, expectedoutput
}

func TestGenerateGaudiNet(t *testing.T) {
	nwconfigs, expectedoutput := fakenetworkconfigs()

	json, err := GenerateGaudiNet(nwconfigs)
	if string(json) != expectedoutput {
		t.Errorf("Expected result '%s', returned '%s': %v", expectedoutput, json, err)
	}
}

func TestGenerateGaudiNetMissingLocalAddr(t *testing.T) {
	nwconfigs, _ := fakenetworkconfigs()

	emptyOutput := "{\n  \"NIC_NET_CONFIG\": []\n}\n"

	nwconfigs["eth1234"].localAddr = nil

	json, _ := GenerateGaudiNet(nwconfigs)
	if string(json) != emptyOutput {
		t.Errorf("Got invalid GaudiNet output '%s' vs '%s'", json, emptyOutput)
	}

	validIp := net.IPv4(10, 120, 0, 1)
	nwconfigs["eth1234"].localAddr = &validIp
	nwconfigs["eth1234"].peerHWAddr = nil

	json, _ = GenerateGaudiNet(nwconfigs)
	if string(json) != emptyOutput {
		t.Errorf("Got invalid GaudiNet output '%s' vs '%s'", json, emptyOutput)
	}
}

func TestWriteGaudiNet(t *testing.T) {
	dir, err := os.MkdirTemp("", "gaudinet.")
	if err != nil {
		t.Errorf("cannot create tmp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	file := filepath.Join(dir, "gaudinet.txt")
	nwconfigs, expectedoutput := fakenetworkconfigs()

	if err := WriteGaudiNet(file, nwconfigs); err != nil {
		t.Errorf("cannot write to '%s': %v", file, err)
	}

	if stat, err := os.Stat(file); err == nil {
		if stat.Mode() != 0644 {
			t.Errorf("file mode is not 0644: %v", stat.Mode())
		}
	} else {
		t.Errorf("could not stat the gaudinet file: %v", err)
	}

	json, err := os.ReadFile(file)
	if err != nil {
		t.Errorf("could not read tmp gaudinet file: %v", err)
	}

	if string(json) != expectedoutput {
		t.Errorf("Expected tmp file contents '%s', returned '%s': %v", expectedoutput, json, err)
	}
}

func TestWriteGaudiNetErrors(t *testing.T) {
	dir, err := os.MkdirTemp("", "gaudinet.")
	if err != nil {
		t.Errorf("cannot create tmp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	nwconfigs, _ := fakenetworkconfigs()

	err = WriteGaudiNet("", nwconfigs)
	if err == nil {
		t.Error("Write succeeded with empty filename")
	}
}

func TestGaudiNetMarshalErrors(t *testing.T) {
	JsonMarshal = func(v any) ([]byte, error) {
		return nil, fmt.Errorf("error")
	}

	dir, err := os.MkdirTemp("", "gaudinet.")
	if err != nil {
		t.Errorf("cannot create tmp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	nwconfigs, _ := fakenetworkconfigs()
	file := filepath.Join(dir, "gaudinet.txt")

	err = WriteGaudiNet(file, nwconfigs)
	if err == nil {
		t.Error("Write succeeded while it should not have")
	}
}
