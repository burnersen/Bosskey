// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Fehler: was der Nutzer sieht, was ins Log kommt, und die eine Stelle,
// an der sie angezeigt werden.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"runtime/debug"
)

// UserError ist ein Fehler, mit dem der Nutzer etwas anfangen kann.
//
//	Msg  was passiert ist, in klaren Worten ("Desktop 4 gibt es nicht")
//	Hint was er tun kann ("Leg ihn mit Win+Strg+D an.")
//	Err  die technische Ursache: kommt ins Log und bleibt für errors.Is
//	     und errors.As erreichbar, ist aber nie die Überschrift
type UserError struct {
	Msg  string
	Hint string
	Err  error
}

func (e *UserError) Error() string {
	if e.Err == nil {
		return e.Msg
	}
	return e.Msg + ": " + e.Err.Error()
}

func (e *UserError) Unwrap() error { return e.Err }

// userError hängt an err eine Meldung und einen Hinweis für den Nutzer.
func userError(err error, msg, hint string) error {
	return &UserError{Msg: msg, Hint: hint, Err: err}
}

// usageError ist ein falscher Aufruf (Exit-Code 2). Der Text darf leer sein,
// wenn das flag-Paket das Problem schon erklärt hat.
type usageError string

func (e usageError) Error() string { return string(e) }

// fileError erklärt die üblichen Dateiprobleme so, dass man etwas tun kann.
// Alles andere behält seinen technischen Text, bekommt aber einen Hinweis.
func fileError(err error, action, path string) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return userError(err, fmt.Sprintf("Kann %s nicht %s: gibt es nicht", path, action),
			"Prüf Namen und Ordner.")
	case errors.Is(err, fs.ErrPermission):
		return userError(err, fmt.Sprintf("Kann %s nicht %s: keine Berechtigung", path, action),
			"Prüf, ob du darauf zugreifen darfst und ob ein anderes Programm die Datei offen hat.")
	default:
		return userError(err, fmt.Sprintf("Kann %s nicht %s", path, action),
			"Versuch es nochmal. Klappt es dauerhaft nicht, steht im Log, woran es liegt.")
	}
}

// finish macht aus dem Ergebnis eines Laufs den Exit-Code und sagt dem
// Nutzer, was passiert ist. Die einzige Stelle, an der Fehler angezeigt
// werden: darunter werden sie nur (mit Zusammenhang) zurückgegeben.
func finish(out *output, logger *slog.Logger, logFile string, err error) int {
	if err == nil {
		logger.Info("done")
		return exitOK
	}

	var usage usageError
	if errors.As(err, &usage) {
		if usage != "" {
			out.Error(string(usage))
		}
		return exitUsage
	}

	var ue *UserError
	if errors.As(err, &ue) {
		out.Error(ue.Msg)
		if ue.Hint != "" {
			out.Hint(ue.Hint)
		}
		logger.Error(ue.Msg, causeAttr(ue.Err)...)
		return exitError
	}

	// Noch nicht erklärt: trotzdem zeigen (nie still bleiben), aufs Log zeigen.
	out.Error("Etwas ist schiefgegangen: " + err.Error())
	if logFile != "" {
		out.Hint("Einzelheiten stehen im Log: " + logFile)
	}
	logger.Error("unexpected error", "err", err)
	return exitError
}

// causeAttr ist die technische Ursache als Log-Feld, oder nichts.
func causeAttr(err error) []any {
	if err == nil {
		return nil
	}
	return []any{"cause", err.Error()}
}

// catchCrash macht aus einem panic eine Meldung und Exit-Code 1. Muss der
// erste Defer in run sein (nach dem Meldungsfenster), damit es jeden panic sieht.
func catchCrash(out *output, logger **slog.Logger, code *int) {
	r := recover()
	if r == nil {
		return
	}
	(*logger).Error("crash", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	out.Error(fmt.Sprintf("%s wurde wegen eines internen Fehlers beendet: %v", appName, r))
	out.Hint("Die Einzelheiten stehen im Log. Einfach neu starten.")
	*code = exitError
}
