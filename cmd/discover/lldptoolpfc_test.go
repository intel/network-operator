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
	"os"
	"testing"
)

type testTable struct {
	arg      string
	success  bool
	expected string
}

func TestVerifyPFCArgument(t *testing.T) {
	tests := []testTable{
		{"0,1", true, "0,1"},
		{"6", true, "6"},
		{"foo,bar", false, ""},
		{" 1, 5       ,  7   ", true, "1,5,7"},
		{"2,6,3,bar", false, ""},
		{"10,1,3", false, ""},
		{"9", false, ""},
		{"-9", false, ""},
		{"1,2,-42", false, ""},
		{"7,5,3,6,4", true, "3,4,5,6,7"},
		{"   7,5    ,  3 , 6 , 4                ", true, "3,4,5,6,7"},
	}

	for _, tt := range tests {
		str, err := VerifyPFCArgument(tt.arg)
		if (tt.success && err != nil) || (!tt.success && err == nil) {
			t.Errorf("verification of %v failed", tt)
		}
		if tt.success && str != tt.expected {
			t.Errorf("expected '%s', received '%s'", tt.expected, str)
		}
	}
}

const (
	lldpBinarySuccess = "true"
	lldpBinaryFailure = "false"
)

func TestLookupLLDPTool(t *testing.T) {
	lldpBinary = lldpBinarySuccess
	if err := LookupLLDPTool(); err != nil {
		t.Errorf("lldp binary '%s' not found at path '%s': %v", lldpBinary, os.Getenv("PATH"), err)
	}
	lldpBinary = lldpBinaryFailure
	if err := LookupLLDPTool(); err != nil {
		t.Errorf("lldp binary '%s' not found at path '%s': %v", lldpBinary, os.Getenv("PATH"), err)
	}
}

func TestEnablePFC(t *testing.T) {
	lldpBinary = lldpBinarySuccess
	if err := LookupLLDPTool(); err != nil {
		t.Errorf("lldp binary '%s' not found at path '%s': %v", lldpBinary, os.Getenv("PATH"), err)
	}

	if err := execPFC("foo0", "1,2"); err != nil {
		t.Errorf("%s enabling failed: %v", lldpBinary, err)
	}
	if err := execPFC("foo0", pfcDisable); err != nil {
		t.Errorf("%s disabling failed: %v", lldpBinary, err)
	}

	lldpBinary = lldpBinaryFailure
	if err := LookupLLDPTool(); err != nil {
		t.Errorf("lldp binary '%s' not found at path '%s': %v", lldpBinary, os.Getenv("PATH"), err)
	}

	if err := execPFC("foo0", "1,2"); err == nil {
		t.Errorf("%s enabling returned success when it should not have", lldpBinary)
	}
	if err := execPFC("foo0", pfcDisable); err == nil {
		t.Errorf("%s disabling returned success when it should not have", lldpBinary)
	}
}
