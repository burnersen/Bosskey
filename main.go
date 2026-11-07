// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Bosskey sitzt im Infobereich der Taskleiste und springt per Tastendruck
// auf einen virtuellen Desktop deiner Wahl. Gedacht als Boss-Taste: eine
// Taste, und du bist sofort auf Desktop 1.
//
// Wo was steht (Vorlage go-cli des dev-starterkit, für ein Tray-Programm
// umgebaut):
//
//	main.go             Start, Schalter, Exit-Codes; nur hier steht os.Exit
//	errors.go           UserError: was der Nutzer sieht, was nur ins Log kommt
//	output.go           Textausgabe für -version und -pruefen
//	logging.go          Log mit Rotation          (unverändert aus dem Kit)
//	paths.go            wo Einstellungen und Log liegen (unverändert aus dem Kit)
//	files.go            atomar schreiben          (unverändert aus dem Kit)
//	config.go           INI: Einstellungen aus der Tabelle, Abschnitt [Tasten]
//	hotkey.go           "Strg+Alt+1" lesen und wieder schreiben
//	desktop.go          Rechnen mit Desktops, ohne Windows (testbar)
//	win32types.go       Strukturen der Windows-API (Größen per Test geprüft)
//	*_windows.go        alles, was Windows direkt anspricht
//	*_other.go          Ersatz für Linux, damit Tests und vet dort laufen
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// version setzt der Release-Bau mit
//
//	go build -ldflags "-X main.version=1.2.3"
//
// Lokal gebaut heißt es "dev". Nie von Hand ändern: die Version steht nur im Tag.
var version = "dev"

const (
	appName = "Bosskey" // für Nutzer, Dateinamen unter Windows
	appID   = "bosskey" // klein: Log-Datei, Linux-Pfade
)

// instanceID verhindert, dass Bosskey zweimal läuft. Nie ändern, sonst
// findet eine neue Version eine noch laufende alte nicht.
const instanceID = "Bosskey-5f0c1d8e-2b7a-4c3e-9a61-7d4e8b2f6c19"

// Exit-Codes. Sie gehören zur Schnittstelle (CI, Skripte), README und Hilfe
// nennen sie. Eigene Codes ab 10.
const (
	exitOK    = 0 // alles erledigt
	exitError = 1 // etwas ging schief, die Meldung sagt, was zu tun ist
	exitUsage = 2 // falscher Aufruf: unbekannter Schalter, überzählige Angabe
)

func main() {
	args := os.Args[1:]
	// Ohne Schalter ist Bosskey ein Tray-Programm ohne Konsole. Mit Schalter
	// (-version, -pruefen) soll die Antwort in der Konsole ankommen, aus der
	// es gestartet wurde.
	if len(args) > 0 {
		attachParentConsole()
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

type options struct {
	showVersion bool
	debug       bool
	check       bool // -pruefen: Einstellungen prüfen, nichts ändern, Ende
	probe       bool // -probelauf: Tray starten und nach ein paar Sekunden beenden (CI)
}

// run ist main ohne os.Exit: Aufräumen läuft immer, und Tests rufen es mit
// eigenen Argumenten und Writern auf.
func run(args []string, stdout, stderr io.Writer) (code int) {
	opts, flagErr := parseFlags(args, stderr)

	// Im Tray-Betrieb gibt es keine Konsole. Was sonst auf dem Bildschirm
	// stünde, wird gesammelt und am Ende als Meldungsfenster gezeigt, aber
	// nur bei einem Fehler. Beim Probelauf (CI) nie ein Fenster: dort klickt
	// niemand auf OK.
	trayMode := flagErr == nil && !opts.showVersion && !opts.check
	var box bytes.Buffer
	out := newOutput(stdout, stderr)
	if trayMode && !opts.probe {
		out = newOutput(&box, &box)
		defer func() {
			if code != exitOK && box.Len() > 0 {
				showMessageBox(strings.TrimSpace(box.String()))
			}
		}()
	}

	logger := slog.New(slog.DiscardHandler)
	// Muss nach dem Meldungsfenster stehen: Defer laufen rückwärts, so
	// setzt catchCrash erst den Code und das Fenster zeigt danach die Meldung.
	defer catchCrash(out, &logger, &code)

	if errors.Is(flagErr, flag.ErrHelp) {
		return exitOK
	}
	if flagErr != nil {
		return finish(out, logger, "", flagErr)
	}
	if opts.showVersion {
		fmt.Fprintf(stdout, "%s %s\n", appName, version)
		return exitOK
	}

	paths, err := defaultPaths()
	if err != nil {
		return finish(out, logger, "", err)
	}
	level := slog.LevelInfo
	if opts.debug {
		level = slog.LevelDebug
	}
	l, closeLog, err := openLog(paths.LogFile, level)
	if err != nil {
		// Ohne Log geht es trotzdem, im Tray-Betrieb steht es im Log-Menü.
		out.Warn("Keine Log-Datei (" + err.Error() + "). Es geht ohne weiter.")
	} else {
		logger = l
		defer closeLog()
	}
	logger.Info("start", "version", version, "args", args)

	if opts.check {
		return checkConfig(out, logger, paths)
	}

	err = runTray(trayOptions{paths: paths, logger: logger, probe: opts.probe})
	return finish(out, logger, paths.LogFile, err)
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&opts.showVersion, "version", false, "Version zeigen und beenden")
	fs.BoolVar(&opts.debug, "debug", false, "mehr Einzelheiten ins Log schreiben")
	fs.BoolVar(&opts.check, "pruefen", false, "Einstellungen prüfen (ändert nichts) und beenden")
	fs.BoolVar(&opts.probe, "probelauf", false, "starten und nach ein paar Sekunden von selbst beenden (für die CI)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "%s %s\n\nAufruf:\n  %s [Schalter]\n\nOhne Schalter startet Bosskey im Infobereich der Taskleiste.\n\nSchalter:\n", appName, version, appName)
		fs.PrintDefaults()
		fmt.Fprint(stderr, "\nExit-Codes:\n  0 alles erledigt\n  1 etwas ging schief (die Meldung sagt, was zu tun ist)\n  2 falscher Aufruf\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, err
		}
		// Das flag-Paket hat das Problem und die Hilfe schon ausgegeben.
		return opts, usageError("")
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return opts, usageError("Unbekannte Angabe: " + fs.Arg(0))
	}
	return opts, nil
}

// checkConfig liest die Einstellungen, ohne etwas zu schreiben, und sagt,
// was gilt. Exit-Code 1, sobald es etwas zu bemängeln gibt.
func checkConfig(out *output, logger *slog.Logger, paths appPaths) int {
	loaded, err := readConfig(paths.ConfigFile)
	if err != nil {
		return finish(out, logger, paths.LogFile, err)
	}
	if loaded.Missing {
		out.Info("Die Einstellungsdatei gibt es noch nicht, es gelten die Standardwerte: " + paths.ConfigFile)
	} else {
		out.Info("Einstellungen: " + paths.ConfigFile)
	}
	for _, b := range loaded.Config.Bindings {
		out.OK(b.Hotkey.String() + " = " + b.Target.String())
	}
	if len(loaded.Config.Bindings) == 0 {
		loaded.Warnings = append(loaded.Warnings, "Keine Taste eingestellt: im Abschnitt [Tasten] fehlt eine Zeile wie \"Pause = 1\".")
	}
	for _, w := range loaded.Warnings {
		out.Warn(w)
		logger.Warn("config", "problem", w)
	}
	if len(loaded.Warnings) > 0 {
		return exitError
	}
	out.OK("Alles in Ordnung.")
	return exitOK
}
