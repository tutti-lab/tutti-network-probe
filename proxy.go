package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type proxySelection struct {
	Source      string
	Address     string
	CurlArgs    []string
	Warning     string
	SystemProxy systemProxySnapshot
}

func resolveProxy(spec string) (proxySelection, error) {
	systemProxy, systemErr := inspectSystemProxy()
	base := proxySelection{SystemProxy: systemProxy}
	inspectionWarning := ""
	if systemErr != nil {
		inspectionWarning = fmt.Sprintf("cannot inspect system proxy: %v", systemErr)
	}

	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "auto":
		if name, value, ok := environmentProxy(); ok {
			base.Source = "environment:" + name
			base.Address = redactProxy(value)
			base.Warning = inspectionWarning
			return base, nil
		}
		if systemErr != nil {
			base.Source = "direct-or-tun"
			base.Warning = inspectionWarning + "; using curl's direct/TUN path"
			return base, nil
		}
		if systemProxy.Enabled && systemProxy.Kind != "pac" {
			return selectSystemProxy(systemProxy), nil
		}
		base.Source = "direct-or-tun"
		if systemProxy.Kind == "pac" {
			base.Warning = "a PAC system proxy is enabled, but curl cannot consume PAC directly; set --proxy to the resolved proxy URL"
		}
		return base, nil
	case "system":
		if systemErr != nil {
			return proxySelection{}, fmt.Errorf("resolve system proxy: %w", systemErr)
		}
		if !systemProxy.Enabled || systemProxy.Kind == "pac" {
			return proxySelection{}, fmt.Errorf("system proxy requested but no supported HTTPS or SOCKS proxy is enabled")
		}
		return selectSystemProxy(systemProxy), nil
	case "environment", "env":
		name, value, ok := environmentProxy()
		if !ok {
			return proxySelection{}, fmt.Errorf("environment proxy requested but HTTPS_PROXY/ALL_PROXY is not set")
		}
		base.Source = "environment:" + name
		base.Address = redactProxy(value)
		base.Warning = inspectionWarning
		return base, nil
	case "direct":
		base.Source = "direct-or-tun"
		base.CurlArgs = []string{"--noproxy", "*"}
		base.Warning = inspectionWarning
		return base, nil
	default:
		parsed, err := url.Parse(spec)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return proxySelection{}, fmt.Errorf("proxy must be auto, system, environment, direct, or a proxy URL")
		}
		base.Source = "explicit"
		base.Address = redactProxy(spec)
		base.CurlArgs = []string{"--proxy", spec, "--noproxy", ""}
		base.Warning = inspectionWarning
		return base, nil
	}
}

func selectSystemProxy(systemProxy systemProxySnapshot) proxySelection {
	scheme := "http"
	if systemProxy.Kind == "socks" {
		scheme = "socks5h"
	}
	address := fmt.Sprintf("%s://%s:%d", scheme, systemProxy.Host, systemProxy.Port)
	return proxySelection{
		Source:      "system:" + systemProxy.Kind,
		Address:     address,
		CurlArgs:    []string{"--proxy", address, "--noproxy", ""},
		SystemProxy: systemProxy,
	}
}

type systemProxySnapshot struct {
	Known   bool
	Enabled bool
	Kind    string
	Host    string
	Port    int
}

func systemProxySummary(snapshot systemProxySnapshot) string {
	if !snapshot.Known {
		return "unknown"
	}
	if !snapshot.Enabled {
		return "disabled"
	}
	if snapshot.Port == 0 {
		return snapshot.Kind
	}
	return fmt.Sprintf("%s:%s:%d", snapshot.Kind, snapshot.Host, snapshot.Port)
}

func environmentProxy() (string, string, bool) {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return name, value, true
		}
	}
	return "", "", false
}

func redactProxy(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "redacted"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func redactProxySpec(spec string) string {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "auto", "system", "environment", "env", "direct":
		return strings.ToLower(strings.TrimSpace(spec))
	default:
		return redactProxy(spec)
	}
}
