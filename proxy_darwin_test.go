//go:build darwin

package main

import "testing"

func TestParseScutilProxy(t *testing.T) {
	values := parseScutilProxy(`<dictionary> {
  HTTPEnable : 1
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7897
  HTTPSProxy : 127.0.0.1
}`)
	if values["HTTPSEnable"] != "1" || values["HTTPSProxy"] != "127.0.0.1" || values["HTTPSPort"] != "7897" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestSystemProxyFromScutilRecordsEnabledPort(t *testing.T) {
	snapshot, err := systemProxyFromScutil(map[string]string{
		"HTTPSEnable": "1",
		"HTTPSProxy":  "127.0.0.1",
		"HTTPSPort":   "7897",
	})
	if err != nil {
		t.Fatalf("systemProxyFromScutil() error: %v", err)
	}
	if !snapshot.Known || !snapshot.Enabled || snapshot.Kind != "https" || snapshot.Host != "127.0.0.1" || snapshot.Port != 7897 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestSystemProxyFromScutilRejectsInvalidPort(t *testing.T) {
	_, err := systemProxyFromScutil(map[string]string{
		"HTTPSEnable": "1",
		"HTTPSProxy":  "127.0.0.1",
		"HTTPSPort":   "invalid",
	})
	if err == nil {
		t.Fatal("systemProxyFromScutil() accepted an invalid port")
	}
}
