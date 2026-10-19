package main

import (
	"runtime"
	"runtime/debug"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/desktop-native
//
// Without it, a development build reports "dev".
var version = "dev"

// appVersion describes the build for the About row in settings: the version,
// then either the module version recorded by the go command or "dev".
func appVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// platformLabel names the OS and architecture the binary runs on.
func platformLabel() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
