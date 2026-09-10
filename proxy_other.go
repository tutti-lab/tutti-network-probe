//go:build !darwin

package main

func inspectSystemProxy() (systemProxySnapshot, error) {
	return systemProxySnapshot{}, nil
}
