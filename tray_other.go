// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

//go:build !windows

// Ersatz für andere Systeme: virtuelle Desktops wie unter Windows gibt es
// dort nicht. Die Datei gibt es nur, damit Logik, Tests und vet unter Linux
// laufen (die CI testet dort).
package main

import "log/slog"

type trayOptions struct {
	paths  appPaths
	logger *slog.Logger
	probe  bool
}

func runTray(trayOptions) error {
	return userError(nil, appName+" läuft nur unter Windows",
		"Unter Linux gehen nur -version und -pruefen.")
}

func attachParentConsole() {}

// showMessageBox: ohne Windows gibt es kein Meldungsfenster, der Text ist
// schon auf stderr gelandet.
func showMessageBox(string) {}
