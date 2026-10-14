// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

// Command raumfeld-connect bridges Teufel Raumfeld multiroom speakers to
// other ecosystems. It talks to the speakers locally — over UPnP AV and the
// Raumfeld host's HTTP API — and has no cloud dependency.
//
// This is the project scaffold: the binary reports its build info and
// exits. The bridge itself is not implemented yet.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/SukramJ/go-raumfeld-connect/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print build info and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	logger.Info("raumfeld-connect.boot", slog.String("build", version.String()))
	logger.Warn("raumfeld-connect.not_implemented")
}
