// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Das Programm als Ganzes, so wie die CI und der Nutzer es aufrufen.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runTool startet das Programm mit seinen Dateien in einem Testordner.
func runTool(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv(homeEnv, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := runTool(t, "-version")
	if code != exitOK || out != "Bosskey dev\n" {
		t.Fatalf("Code %d, Ausgabe %q", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"-quatsch"}, {"irgendwas"}} {
		code, _, errOut := runTool(t, args...)
		if code != exitUsage {
			t.Errorf("%v: Code %d, erwartet %d", args, code, exitUsage)
		}
		if !strings.Contains(errOut, "Exit-Codes:") {
			t.Errorf("%v: keine Hilfe: %q", args, errOut)
		}
	}
}

func TestHelp(t *testing.T) {
	code, _, errOut := runTool(t, "-h")
	if code != exitOK || !strings.Contains(errOut, "-pruefen") {
		t.Fatalf("Code %d, %q", code, errOut)
	}
}

func TestCheckDefaults(t *testing.T) {
	code, out, errOut := runTool(t, "-pruefen")
	if code != exitOK {
		t.Fatalf("Code %d, %q %q", code, out, errOut)
	}
	if !strings.Contains(out, "[OK] Pause = Desktop 1") || !strings.Contains(out, "gibt es noch nicht") {
		t.Fatalf("Ausgabe %q", out)
	}
	// -pruefen legt nichts an.
	if _, err := os.Stat(filepath.Join(os.Getenv(homeEnv), appName+".ini")); err == nil {
		t.Fatal("-pruefen hat die Einstellungsdatei angelegt")
	}
}

func TestCheckFindsProblems(t *testing.T) {
	home := t.TempDir()
	ini := "meldungBeimStart=true\nnochmalZurueck=false\n[Tasten]\nStrg+Blubb = 1\n"
	if err := os.WriteFile(filepath.Join(home, appName+".ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(homeEnv, home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-pruefen"}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("Code %d", code)
	}
	if !strings.Contains(stderr.String(), "Zeile 4") || !strings.Contains(stderr.String(), "Keine Taste eingestellt") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestTrayOutsideWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unter Windows startet hier der echte Tray, das prüft der Probelauf der CI")
	}
	code, _, errOut := runTool(t, "-probelauf")
	if code != exitError || !strings.Contains(errOut, "nur unter Windows") {
		t.Fatalf("Code %d, %q", code, errOut)
	}
}
