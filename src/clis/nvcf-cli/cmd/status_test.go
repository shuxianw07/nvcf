/*
SPDX-FileCopyrightText: Copyright (c) NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nvcf-cli/internal/state"
)

func TestRunStatusOutputs(t *testing.T) {
	tests := []struct {
		name      string
		json      bool
		checkText []string
	}{
		{
			name: "JSON output",
			json: true,
		},
		{
			name: "human readable output",
			checkText: []string{
				"Config File: ", "Cluster Mode: true", "Admin Token: abcdefghij**********qrstuvwxyz [❌ Expired]",
				"API Key: 1234567890**********1234567890 [❌ Expired]", "Function ID: function-123",
				"Version ID: version-456", "Name: sample-function", "nvcf-cli function deploy",
				"Base URL: https://api.example.test", "Invoke URL: https://invoke.example.test",
				"Account: account-789", "Admin token has expired", "API key has expired",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			oldCfgFile := cfgFile
			oldManager, oldManagerKey := configStateManager, configStateManagerKey
			oldDefaultManager := state.DefaultStateManager
			oldJSONOutput, oldShowTokens := jsonOutput, statusShowTokens
			t.Cleanup(func() {
				cfgFile = oldCfgFile
				configStateManager, configStateManagerKey = oldManager, oldManagerKey
				state.DefaultStateManager = oldDefaultManager
				jsonOutput, statusShowTokens = oldJSONOutput, oldShowTokens
			})

			cfgFile = filepath.Join(home, "status.yaml")
			configStateManager = state.NewStateManagerForConfig(cfgFile)
			configStateManagerKey = cfgFile
			state.DefaultStateManager = state.NewStateManager()
			manager := configStateManager
			expired := time.Now().Add(-time.Hour)
			manager.SetTokens("abcdefghijABCDEFGHIJqrstuvwxyz", "1234567890ABCDEFGHIJ1234567890", expired, expired)
			manager.SetConfig(cfgFile, filepath.Join(home, ".kube", "config"), true)
			manager.SetFunction("function-123", "version-456", "sample-function")
			manager.SetEndpoints("https://api.example.test", "https://invoke.example.test", "account-789")
			if err := manager.Save(); err != nil {
				t.Fatalf("save fixture state: %v", err)
			}
			jsonOutput = tt.json
			statusShowTokens = false

			oldStdout := os.Stdout
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatalf("create stdout pipe: %v", err)
			}
			os.Stdout = writer
			err = runStatus(nil, nil)
			_ = writer.Close()
			os.Stdout = oldStdout
			if err != nil {
				t.Fatalf("runStatus: %v", err)
			}
			output, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				t.Fatalf("read status output: %v", err)
			}
			text := string(output)

			if tt.json {
				var payload struct {
					Config map[string]interface{} `json:"config"`
					Auth struct {
						AdminToken map[string]interface{} `json:"adminToken"`
						APIKey     map[string]interface{} `json:"apiKey"`
					} `json:"auth"`
					Function map[string]interface{} `json:"currentFunction"`
					Endpoints map[string]interface{} `json:"endpoints"`
					Warnings []string `json:"warnings"`
				}
				if err := json.Unmarshal(output, &payload); err != nil {
					t.Fatalf("decode status JSON: %v\n%s", err, text)
				}
				if payload.Auth.AdminToken["status"] != "expired" || payload.Auth.APIKey["status"] != "expired" {
					t.Errorf("credential statuses = %v / %v, want expired / expired", payload.Auth.AdminToken["status"], payload.Auth.APIKey["status"])
				}
				if payload.Auth.AdminToken["value"] != maskToken("abcdefghijABCDEFGHIJqrstuvwxyz") || payload.Auth.APIKey["value"] != maskToken("1234567890ABCDEFGHIJ1234567890") {
					t.Errorf("credential values were not masked: %v / %v", payload.Auth.AdminToken["value"], payload.Auth.APIKey["value"])
				}
				if payload.Function["hasFunction"] != true || payload.Function["functionId"] != "function-123" || payload.Function["versionId"] != "version-456" || payload.Function["name"] != "sample-function" {
					t.Errorf("current function = %v", payload.Function)
				}
				if payload.Config["configFile"] != cfgFile || payload.Endpoints["baseUrl"] != "https://api.example.test" || payload.Endpoints["invokeUrl"] != "https://invoke.example.test" || payload.Endpoints["account"] != "account-789" {
					t.Errorf("config/endpoints = %v / %v", payload.Config, payload.Endpoints)
				}
				if !containsStatusWarning(payload.Warnings, "Admin token has expired") || !containsStatusWarning(payload.Warnings, "API key has expired") {
					t.Errorf("warnings = %v", payload.Warnings)
				}
				return
			}

			for _, expected := range tt.checkText {
				if !strings.Contains(text, expected) {
					t.Errorf("status output missing %q", expected)
				}
			}
		})
	}
}

func containsStatusWarning(warnings []string, expected string) bool {
	for _, warning := range warnings {
		if warning == expected {
			return true
		}
	}
	return false
}
