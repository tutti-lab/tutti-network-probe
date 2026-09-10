package main

import (
	"math"
	"testing"
)

func TestParseCurlMetrics(t *testing.T) {
	metrics := parseCurlMetrics(`http_code=000
http_connect=200
remote_ip=127.0.0.1
remote_port=7897
local_ip=127.0.0.1
local_port=59153
time_namelookup=0.000100
time_connect=0.000374
time_appconnect=0.000000
time_pretransfer=0.000000
time_starttransfer=0.000000
time_total=5.002895
`)

	if metrics.HTTPConnectCode != 200 {
		t.Fatalf("HTTPConnectCode = %d, want 200", metrics.HTTPConnectCode)
	}
	if metrics.LocalPort != 59153 {
		t.Fatalf("LocalPort = %d, want 59153", metrics.LocalPort)
	}
	if metrics.ConnectSeconds != 0.000374 {
		t.Fatalf("ConnectSeconds = %f, want 0.000374", metrics.ConnectSeconds)
	}
	if metrics.TotalSeconds != 5.002895 {
		t.Fatalf("TotalSeconds = %f, want 5.002895", metrics.TotalSeconds)
	}
}

func TestClassifyTLSHandshakeTimeout(t *testing.T) {
	metrics := curlMetrics{ConnectSeconds: 0.000374, TLSSeconds: 0}
	if got := classifyFailure(28, metrics); got != "tls_timeout" {
		t.Fatalf("classifyFailure() = %q, want tls_timeout", got)
	}
}

func TestTLSPhaseSeconds(t *testing.T) {
	metrics := curlMetrics{ConnectSeconds: 0.003479, TLSSeconds: 0.174201}
	if got := tlsPhaseSeconds(metrics); math.Abs(got-0.170722) > 0.0000001 {
		t.Fatalf("tlsPhaseSeconds() = %f, want 0.170722", got)
	}
}

func TestParseTargetRequiresHTTPS(t *testing.T) {
	if _, err := parseTarget("example=http://example.com"); err == nil {
		t.Fatal("parseTarget() accepted an HTTP URL")
	}
	if _, err := parseTarget("example=https://example.com/path"); err != nil {
		t.Fatalf("parseTarget() rejected an HTTPS URL: %v", err)
	}
}

func TestParseTargetRejectsTerminalControlCharacters(t *testing.T) {
	if _, err := parseTarget("bad\nname=https://example.com"); err == nil {
		t.Fatal("parseTarget() accepted a newline in the target name")
	}
}
