//go:build darwin

package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func inspectSystemProxy() (systemProxySnapshot, error) {
	output, err := exec.Command("/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return systemProxySnapshot{}, err
	}
	return systemProxyFromScutil(parseScutilProxy(string(output)))
}

func systemProxyFromScutil(values map[string]string) (systemProxySnapshot, error) {
	if values["HTTPSEnable"] == "1" && values["HTTPSProxy"] != "" && values["HTTPSPort"] != "" {
		port, err := parseProxyPort(values["HTTPSPort"])
		if err != nil {
			return systemProxySnapshot{}, err
		}
		return systemProxySnapshot{Known: true, Enabled: true, Kind: "https", Host: values["HTTPSProxy"], Port: port}, nil
	}
	if values["SOCKSEnable"] == "1" && values["SOCKSProxy"] != "" && values["SOCKSPort"] != "" {
		port, err := parseProxyPort(values["SOCKSPort"])
		if err != nil {
			return systemProxySnapshot{}, err
		}
		return systemProxySnapshot{Known: true, Enabled: true, Kind: "socks", Host: values["SOCKSProxy"], Port: port}, nil
	}
	if values["ProxyAutoConfigEnable"] == "1" {
		return systemProxySnapshot{Known: true, Enabled: true, Kind: "pac"}, nil
	}
	return systemProxySnapshot{Known: true}, nil
}

func parseProxyPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid system proxy port %q", value)
	}
	return port, nil
}

func parseScutilProxy(output string) map[string]string {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}
