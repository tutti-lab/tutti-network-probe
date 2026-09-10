package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const curlWriteOut = `http_code=%{http_code}
http_connect=%{http_connect}
remote_ip=%{remote_ip}
remote_port=%{remote_port}
local_ip=%{local_ip}
local_port=%{local_port}
http_version=%{http_version}
num_connects=%{num_connects}
ssl_verify_result=%{ssl_verify_result}
url_effective=%{url_effective}
time_namelookup=%{time_namelookup}
time_connect=%{time_connect}
time_appconnect=%{time_appconnect}
time_pretransfer=%{time_pretransfer}
time_starttransfer=%{time_starttransfer}
time_total=%{time_total}
`

type target struct {
	Name string
	URL  string
}

type curlMetrics struct {
	HTTPCode             int
	HTTPConnectCode      int
	RemoteIP             string
	RemotePort           int
	LocalIP              string
	LocalPort            int
	HTTPVersion          string
	NumConnects          int
	SSLVerifyResult      int
	EffectiveURL         string
	NameLookupSeconds    float64
	ConnectSeconds       float64
	TLSSeconds           float64
	PretransferSeconds   float64
	StartTransferSeconds float64
	TotalSeconds         float64
}

type proxyLog struct {
	Source             string `json:"source"`
	Address            string `json:"address,omitempty"`
	SystemProxyKnown   bool   `json:"system_proxy_known"`
	SystemProxyEnabled bool   `json:"system_proxy_enabled"`
	SystemProxyKind    string `json:"system_proxy_kind,omitempty"`
	SystemProxyHost    string `json:"system_proxy_host,omitempty"`
	SystemProxyPort    int    `json:"system_proxy_port,omitempty"`
}

type timingLog struct {
	NameLookupSeconds    float64 `json:"namelookup_seconds"`
	ConnectSeconds       float64 `json:"connect_seconds"`
	TLSSeconds           float64 `json:"tls_seconds"`
	PretransferSeconds   float64 `json:"pretransfer_seconds"`
	StartTransferSeconds float64 `json:"starttransfer_seconds"`
	TotalSeconds         float64 `json:"total_seconds"`
}

type observation struct {
	StartedAt       string    `json:"started_at"`
	CompletedAt     string    `json:"completed_at"`
	Target          string    `json:"target"`
	URL             string    `json:"url"`
	Status          string    `json:"status"`
	Category        string    `json:"category,omitempty"`
	Error           string    `json:"error,omitempty"`
	CurlExitCode    int       `json:"curl_exit_code"`
	Proxy           proxyLog  `json:"proxy"`
	HTTPCode        int       `json:"http_code"`
	HTTPConnectCode int       `json:"http_connect_code"`
	RemoteIP        string    `json:"remote_ip,omitempty"`
	RemotePort      int       `json:"remote_port,omitempty"`
	LocalIP         string    `json:"local_ip,omitempty"`
	LocalPort       int       `json:"local_port,omitempty"`
	HTTPVersion     string    `json:"http_version,omitempty"`
	NumConnects     int       `json:"num_connects"`
	SSLVerifyResult int       `json:"ssl_verify_result"`
	EffectiveURL    string    `json:"effective_url,omitempty"`
	Timings         timingLog `json:"timings"`
}

type probeResult struct {
	Target    target
	Completed time.Time
	Metrics   curlMetrics
	Record    observation
}

func defaultTargets() []target {
	return []target{
		{Name: "google", URL: "https://www.google.com/generate_204"},
		{Name: "openai", URL: "https://api.openai.com/v1/models"},
		{Name: "anthropic", URL: "https://api.anthropic.com/v1/models"},
		{Name: "e2b", URL: "https://e2b.app"},
	}
}

func parseTarget(value string) (target, error) {
	name, rawURL, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(rawURL) == "" {
		return target{}, fmt.Errorf("target %q must use name=https://host/path", value)
	}
	name = strings.TrimSpace(name)
	for _, character := range name {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '.' && character != '_' && character != '-' {
			return target{}, fmt.Errorf("target name %q may only contain letters, numbers, dot, underscore, and hyphen", name)
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return target{}, fmt.Errorf("target %q must contain a valid HTTPS URL", value)
	}
	return target{Name: name, URL: rawURL}, nil
}

func runCycle(ctx context.Context, cfg config, proxy proxySelection) []probeResult {
	results := make([]probeResult, len(cfg.targets))
	var wg sync.WaitGroup
	for index, target := range cfg.targets {
		index := index
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index] = probeTarget(ctx, cfg, proxy, target)
		}()
	}
	wg.Wait()
	return results
}

func probeTarget(ctx context.Context, cfg config, proxy proxySelection, target target) probeResult {
	args := []string{
		"--silent",
		"--show-error",
		"--output", os.DevNull,
		"--connect-timeout", formatCurlDuration(cfg.connectTimeout),
		"--max-time", formatCurlDuration(cfg.totalTimeout),
		"--user-agent", "tutti-network-probe/" + version,
		"--write-out", curlWriteOut,
	}
	args = append(args, proxy.CurlArgs...)
	args = append(args, target.URL)

	started := time.Now()
	cmd := exec.CommandContext(ctx, cfg.curlPath, args...)
	stdout, err := cmd.Output()
	completed := time.Now()
	metrics := parseCurlMetrics(string(stdout))
	if err == nil {
		return probeResult{
			Target:    target,
			Completed: completed,
			Metrics:   metrics,
			Record:    makeObservation(started, completed, target, proxy, "ok", "", "", 0, metrics),
		}
	}

	exitCode := -1
	errorText := err.Error()
	category := "transport"
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			errorText = stderr
		}
	}
	status := "failure"
	if ctx.Err() != nil {
		status = "cancelled"
		category = "cancelled"
		errorText = ctx.Err().Error()
	} else {
		category = classifyFailure(exitCode, metrics)
	}
	return probeResult{
		Target:    target,
		Completed: completed,
		Metrics:   metrics,
		Record:    makeObservation(started, completed, target, proxy, status, category, errorText, exitCode, metrics),
	}
}

func makeObservation(
	started time.Time,
	completed time.Time,
	target target,
	proxy proxySelection,
	status string,
	category string,
	errorText string,
	exitCode int,
	metrics curlMetrics,
) observation {
	return observation{
		StartedAt:       utcTimestamp(started),
		CompletedAt:     utcTimestamp(completed),
		Target:          target.Name,
		URL:             redactTargetURL(target.URL),
		Status:          status,
		Category:        category,
		Error:           errorText,
		CurlExitCode:    exitCode,
		Proxy:           makeProxyLog(proxy),
		HTTPCode:        metrics.HTTPCode,
		HTTPConnectCode: metrics.HTTPConnectCode,
		RemoteIP:        metrics.RemoteIP,
		RemotePort:      metrics.RemotePort,
		LocalIP:         metrics.LocalIP,
		LocalPort:       metrics.LocalPort,
		HTTPVersion:     metrics.HTTPVersion,
		NumConnects:     metrics.NumConnects,
		SSLVerifyResult: metrics.SSLVerifyResult,
		EffectiveURL:    redactTargetURL(metrics.EffectiveURL),
		Timings: timingLog{
			NameLookupSeconds:    metrics.NameLookupSeconds,
			ConnectSeconds:       metrics.ConnectSeconds,
			TLSSeconds:           metrics.TLSSeconds,
			PretransferSeconds:   metrics.PretransferSeconds,
			StartTransferSeconds: metrics.StartTransferSeconds,
			TotalSeconds:         metrics.TotalSeconds,
		},
	}
}

func makeProxyLog(proxy proxySelection) proxyLog {
	return proxyLog{
		Source:             proxy.Source,
		Address:            proxy.Address,
		SystemProxyKnown:   proxy.SystemProxy.Known,
		SystemProxyEnabled: proxy.SystemProxy.Enabled,
		SystemProxyKind:    proxy.SystemProxy.Kind,
		SystemProxyHost:    proxy.SystemProxy.Host,
		SystemProxyPort:    proxy.SystemProxy.Port,
	}
}

func redactTargetURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "redacted"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func tlsPhaseSeconds(metrics curlMetrics) float64 {
	if metrics.TLSSeconds <= metrics.ConnectSeconds {
		return 0
	}
	return metrics.TLSSeconds - metrics.ConnectSeconds
}

func formatCurlDuration(duration time.Duration) string {
	return strconv.FormatFloat(duration.Seconds(), 'f', 3, 64)
}

func parseCurlMetrics(output string) curlMetrics {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[key] = value
		}
	}
	return curlMetrics{
		HTTPCode:             parseInt(values["http_code"]),
		HTTPConnectCode:      parseInt(values["http_connect"]),
		RemoteIP:             values["remote_ip"],
		RemotePort:           parseInt(values["remote_port"]),
		LocalIP:              values["local_ip"],
		LocalPort:            parseInt(values["local_port"]),
		HTTPVersion:          values["http_version"],
		NumConnects:          parseInt(values["num_connects"]),
		SSLVerifyResult:      parseInt(values["ssl_verify_result"]),
		EffectiveURL:         values["url_effective"],
		NameLookupSeconds:    parseFloat(values["time_namelookup"]),
		ConnectSeconds:       parseFloat(values["time_connect"]),
		TLSSeconds:           parseFloat(values["time_appconnect"]),
		PretransferSeconds:   parseFloat(values["time_pretransfer"]),
		StartTransferSeconds: parseFloat(values["time_starttransfer"]),
		TotalSeconds:         parseFloat(values["time_total"]),
	}
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}

func classifyFailure(exitCode int, metrics curlMetrics) string {
	switch exitCode {
	case 5:
		return "proxy_dns"
	case 6:
		return "dns"
	case 7:
		return "connect"
	case 28:
		if metrics.ConnectSeconds > 0 && metrics.TLSSeconds == 0 {
			return "tls_timeout"
		}
		if metrics.ConnectSeconds == 0 {
			return "connect_timeout"
		}
		return "timeout"
	case 35:
		return "tls_handshake"
	case 52:
		return "empty_reply"
	case 55:
		return "send"
	case 56:
		return "receive"
	case 60, 77:
		return "tls_certificate"
	case 92:
		return "http2"
	default:
		return "transport"
	}
}
