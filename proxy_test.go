package main

import (
	"testing"
)

func TestRedactProxyCredentials(t *testing.T) {
	got := redactProxy("http://user:secret@127.0.0.1:7897/path?token=secret")
	if got != "http://127.0.0.1:7897/path" {
		t.Fatalf("redactProxy() = %q", got)
	}
}

func TestRedactProxySpecCredentials(t *testing.T) {
	got := redactProxySpec("http://user:secret@127.0.0.1:7897")
	if got != "http://127.0.0.1:7897" {
		t.Fatalf("redactProxySpec() = %q", got)
	}
}

func TestResolveProxyPrefersEnvironmentInAutoMode(t *testing.T) {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, "")
	}
	t.Setenv("HTTPS_PROXY", "http://user:secret@127.0.0.1:7897")

	selection, err := resolveProxy("auto")
	if err != nil {
		t.Fatalf("resolveProxy() error: %v", err)
	}
	if selection.Source != "environment:HTTPS_PROXY" {
		t.Fatalf("Source = %q", selection.Source)
	}
	if selection.Address != "http://127.0.0.1:7897" {
		t.Fatalf("Address = %q", selection.Address)
	}
	if len(selection.CurlArgs) != 0 {
		t.Fatalf("CurlArgs = %v, want curl environment handling", selection.CurlArgs)
	}
}

func TestResolveDirectDisablesApplicationProxy(t *testing.T) {
	selection, err := resolveProxy("direct")
	if err != nil {
		t.Fatalf("resolveProxy() error: %v", err)
	}
	if len(selection.CurlArgs) != 2 || selection.CurlArgs[0] != "--noproxy" || selection.CurlArgs[1] != "*" {
		t.Fatalf("CurlArgs = %v", selection.CurlArgs)
	}
}
