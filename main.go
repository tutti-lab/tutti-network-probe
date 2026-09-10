package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

type targetList []target

func (t *targetList) String() string {
	parts := make([]string, 0, len(*t))
	for _, target := range *t {
		parts = append(parts, target.Name+"="+target.URL)
	}
	return strings.Join(parts, ",")
}

func (t *targetList) Set(value string) error {
	target, err := parseTarget(value)
	if err != nil {
		return err
	}
	*t = append(*t, target)
	return nil
}

type config struct {
	curlPath       string
	interval       time.Duration
	connectTimeout time.Duration
	totalTimeout   time.Duration
	logPath        string
	maxLogBytes    int64
	maxLogBackups  int
	proxy          string
	once           bool
	quiet          bool
	showVersion    bool
	targets        []target
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tutti-network-probe: %v\n", err)
		os.Exit(1)
	}
}

func run() (returnErr error) {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		return err
	}
	if cfg.showVersion {
		fmt.Println(version)
		return nil
	}

	resolvedCurl, err := exec.LookPath(cfg.curlPath)
	if err != nil {
		return fmt.Errorf("find curl %q: %w", cfg.curlPath, err)
	}
	cfg.curlPath = resolvedCurl
	curlVersion, err := readCurlVersion(cfg.curlPath)
	if err != nil {
		return err
	}
	sessionID, err := newSessionID()
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}
	logger, err := openJSONLLogger(cfg.logPath, cfg.maxLogBytes, cfg.maxLogBackups)
	if err != nil {
		return err
	}
	stopReason := "completed"
	defer func() {
		if returnErr != nil {
			stopReason = "error"
		}
		stopErr := logger.Append(sessionStopEvent{
			SchemaVersion: logSchemaVersion,
			Event:         "session_stop",
			Timestamp:     utcTimestamp(time.Now()),
			SessionID:     sessionID,
			ProbeVersion:  version,
			Reason:        stopReason,
		})
		closeErr := logger.Close()
		if returnErr == nil && stopErr != nil {
			returnErr = stopErr
		}
		if returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close observation log: %w", closeErr)
		}
	}()

	startTargets := make([]logTarget, 0, len(cfg.targets))
	for _, target := range cfg.targets {
		startTargets = append(startTargets, logTarget{Name: target.Name, URL: redactTargetURL(target.URL)})
	}
	if err := logger.Append(sessionStartEvent{
		SchemaVersion:  logSchemaVersion,
		Event:          "session_start",
		Timestamp:      utcTimestamp(time.Now()),
		SessionID:      sessionID,
		ProbeVersion:   version,
		Hostname:       hostname,
		OS:             runtime.GOOS,
		Architecture:   runtime.GOARCH,
		PID:            os.Getpid(),
		CurlVersion:    curlVersion,
		ProxyMode:      redactProxySpec(cfg.proxy),
		Interval:       cfg.interval.String(),
		ConnectTimeout: cfg.connectTimeout.String(),
		TotalTimeout:   cfg.totalTimeout.String(),
		Targets:        startTargets,
	}); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !cfg.quiet {
		fmt.Printf(
			"tutti-network-probe %s started: interval=%s proxy=%s log=%s targets=%d\n",
			version,
			cfg.interval,
			redactProxySpec(cfg.proxy),
			cfg.logPath,
			len(cfg.targets),
		)
	}

	warnings := make(map[string]struct{})
	var sequence uint64
	for {
		sequence++
		cycleStarted := time.Now()
		selection, err := resolveProxy(cfg.proxy)
		if err != nil {
			return err
		}
		if selection.Warning != "" {
			if _, shown := warnings[selection.Warning]; !shown {
				fmt.Fprintf(os.Stderr, "tutti-network-probe: warning: %s\n", selection.Warning)
				warnings[selection.Warning] = struct{}{}
			}
		}

		results := runCycle(ctx, cfg, selection)
		cycleCompleted := time.Now()
		observations := make([]observation, 0, len(results))
		for _, result := range results {
			observations = append(observations, result.Record)
		}
		if err := logger.Append(probeCycleEvent{
			SchemaVersion:   logSchemaVersion,
			Event:           "probe_cycle",
			SessionID:       sessionID,
			ProbeVersion:    version,
			Sequence:        sequence,
			StartedAt:       utcTimestamp(cycleStarted),
			CompletedAt:     utcTimestamp(cycleCompleted),
			DurationSeconds: cycleCompleted.Sub(cycleStarted).Seconds(),
			Proxy:           makeProxyLog(selection),
			Results:         observations,
		}); err != nil {
			return err
		}

		for _, result := range results {
			if cfg.quiet {
				continue
			}
			switch result.Record.Status {
			case "ok":
				fmt.Printf(
					"%s %s ok: http=%d tls_ready=%.3fs tls_phase=%.3fs total=%.3fs proxy=%s system_proxy=%s\n",
					result.Completed.Format(time.RFC3339Nano),
					result.Target.Name,
					result.Metrics.HTTPCode,
					result.Metrics.TLSSeconds,
					tlsPhaseSeconds(result.Metrics),
					result.Metrics.TotalSeconds,
					selection.Source,
					systemProxySummary(selection.SystemProxy),
				)
			case "failure":
				fmt.Fprintf(
					os.Stderr,
					"%s %s failed: category=%s curl_exit=%d total=%.3fs proxy=%s system_proxy=%s\n",
					result.Completed.Format(time.RFC3339Nano),
					result.Target.Name,
					result.Record.Category,
					result.Record.CurlExitCode,
					result.Metrics.TotalSeconds,
					selection.Source,
					systemProxySummary(selection.SystemProxy),
				)
			}
		}

		if cfg.once || ctx.Err() != nil {
			if ctx.Err() != nil {
				stopReason = "signal"
			}
			return nil
		}

		wait := cfg.interval - time.Since(cycleStarted)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			stopReason = "signal"
			return nil
		case <-timer.C:
		}
	}
}

func parseConfig(args []string) (config, error) {
	defaultLog, err := defaultLogPath()
	if err != nil {
		return config{}, err
	}

	flags := flag.NewFlagSet("tutti-network-probe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var customTargets targetList
	cfg := config{}
	flags.StringVar(&cfg.curlPath, "curl", "curl", "curl executable path")
	flags.DurationVar(&cfg.interval, "interval", 10*time.Second, "time between probe cycle starts")
	flags.DurationVar(&cfg.connectTimeout, "connect-timeout", 8*time.Second, "maximum connection and TLS setup time")
	flags.DurationVar(&cfg.totalTimeout, "timeout", 15*time.Second, "maximum time for one target")
	flags.StringVar(&cfg.logPath, "log-file", defaultLog, "all-observation JSONL log path")
	flags.Int64Var(&cfg.maxLogBytes, "max-log-bytes", 25*1024*1024, "rotate the observation log at this size")
	flags.IntVar(&cfg.maxLogBackups, "max-log-backups", 7, "number of rotated observation logs to retain")
	flags.StringVar(&cfg.proxy, "proxy", "auto", "auto, system, environment, direct, or a proxy URL")
	flags.BoolVar(&cfg.once, "once", false, "run one probe cycle and exit")
	flags.BoolVar(&cfg.quiet, "quiet", false, "suppress per-cycle terminal output")
	flags.BoolVar(&cfg.showVersion, "version", false, "print version and exit")
	flags.Var(&customTargets, "target", "replace defaults with a repeatable name=https://host/path target")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: tutti-network-probe [options]\n\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if cfg.interval <= 0 {
		return config{}, errors.New("interval must be greater than zero")
	}
	if cfg.connectTimeout <= 0 || cfg.totalTimeout <= 0 {
		return config{}, errors.New("timeouts must be greater than zero")
	}
	if cfg.connectTimeout > cfg.totalTimeout {
		return config{}, errors.New("connect-timeout cannot exceed timeout")
	}
	if cfg.logPath == "" {
		return config{}, errors.New("log-file cannot be empty")
	}
	if cfg.maxLogBytes < 1024 {
		return config{}, errors.New("max-log-bytes must be at least 1024")
	}
	if cfg.maxLogBackups < 1 {
		return config{}, errors.New("max-log-backups must be at least 1")
	}
	if len(customTargets) == 0 {
		cfg.targets = defaultTargets()
	} else {
		cfg.targets = customTargets
	}
	return cfg, nil
}

func readCurlVersion(path string) (string, error) {
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("read curl version: %w", err)
	}
	line, _, _ := strings.Cut(string(output), "\n")
	if strings.TrimSpace(line) == "" {
		return "", errors.New("curl --version returned no version")
	}
	return strings.TrimSpace(line), nil
}

func defaultLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Logs", "Tutti", "network-probe.jsonl"), nil
	}
	return filepath.Join(home, ".local", "state", "tutti", "network-probe.jsonl"), nil
}
