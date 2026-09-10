package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const logSchemaVersion = 1

type logTarget struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type sessionStartEvent struct {
	SchemaVersion  int         `json:"schema_version"`
	Event          string      `json:"event"`
	Timestamp      string      `json:"timestamp"`
	SessionID      string      `json:"session_id"`
	ProbeVersion   string      `json:"probe_version"`
	Hostname       string      `json:"hostname"`
	OS             string      `json:"os"`
	Architecture   string      `json:"architecture"`
	PID            int         `json:"pid"`
	CurlVersion    string      `json:"curl_version"`
	ProxyMode      string      `json:"proxy_mode"`
	Interval       string      `json:"interval"`
	ConnectTimeout string      `json:"connect_timeout"`
	TotalTimeout   string      `json:"total_timeout"`
	Targets        []logTarget `json:"targets"`
}

type probeCycleEvent struct {
	SchemaVersion   int           `json:"schema_version"`
	Event           string        `json:"event"`
	SessionID       string        `json:"session_id"`
	ProbeVersion    string        `json:"probe_version"`
	Sequence        uint64        `json:"sequence"`
	StartedAt       string        `json:"started_at"`
	CompletedAt     string        `json:"completed_at"`
	DurationSeconds float64       `json:"duration_seconds"`
	Proxy           proxyLog      `json:"proxy"`
	Results         []observation `json:"results"`
}

type sessionStopEvent struct {
	SchemaVersion int    `json:"schema_version"`
	Event         string `json:"event"`
	Timestamp     string `json:"timestamp"`
	SessionID     string `json:"session_id"`
	ProbeVersion  string `json:"probe_version"`
	Reason        string `json:"reason"`
}

type jsonlLogger struct {
	path       string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

func openJSONLLogger(path string, maxBytes int64, maxBackups int) (*jsonlLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	logger := &jsonlLogger{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
	if err := logger.openAppend(); err != nil {
		return nil, err
	}
	return logger, nil
}

func (logger *jsonlLogger) Append(event any) error {
	if logger.file == nil {
		return fmt.Errorf("observation log %q is closed", logger.path)
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode log event: %w", err)
	}
	line = append(line, '\n')
	if logger.size > 0 && logger.size+int64(len(line)) > logger.maxBytes {
		if err := logger.rotate(); err != nil {
			return err
		}
	}
	written, err := logger.file.Write(line)
	if err != nil {
		return fmt.Errorf("write log event: %w", err)
	}
	if written != len(line) {
		return fmt.Errorf("write log event: wrote %d of %d bytes", written, len(line))
	}
	logger.size += int64(written)
	if err := logger.file.Sync(); err != nil {
		return fmt.Errorf("sync log event: %w", err)
	}
	return nil
}

func (logger *jsonlLogger) Close() error {
	if logger.file == nil {
		return nil
	}
	err := logger.file.Close()
	logger.file = nil
	return err
}

func (logger *jsonlLogger) openAppend() error {
	file, err := os.OpenFile(logger.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open observation log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("secure observation log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("inspect observation log: %w", err)
	}
	logger.file = file
	logger.size = info.Size()
	return nil
}

func (logger *jsonlLogger) rotate() error {
	if err := logger.Close(); err != nil {
		return fmt.Errorf("close observation log before rotation: %w", err)
	}
	for index := logger.maxBackups; index >= 1; index-- {
		source := logger.path
		if index > 1 {
			source = fmt.Sprintf("%s.%d", logger.path, index-1)
		}
		destination := fmt.Sprintf("%s.%d", logger.path, index)
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove oldest observation log %q: %w", destination, err)
		}
		if err := os.Rename(source, destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate observation log %q to %q: %w", source, destination, err)
		}
	}
	return logger.openAppend()
}

func newSessionID() (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("create session id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func utcTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
