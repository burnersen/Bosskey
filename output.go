// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Textausgabe für die Konsole (-version, -pruefen) und, gesammelt, für das
// Meldungsfenster bei einem Startfehler im Tray-Betrieb.
package main

import (
	"fmt"
	"io"
)

// output ist alles, was der Nutzer als Text liest. Ergebnisse auf stdout,
// Warnungen und Fehler auf stderr. Ohne Farben: Bosskey ist ein Fenster-
// programm, die Konsole ist nur Zugabe.
//
// Das Log ist getrennt (logging.go): hier steht, was der Nutzer tun kann,
// im Log, was genau passiert ist.
type output struct {
	stdout, stderr io.Writer
}

func newOutput(stdout, stderr io.Writer) *output {
	return &output{stdout: stdout, stderr: stderr}
}

// OK meldet etwas Erledigtes.
func (o *output) OK(msg string) { fmt.Fprintf(o.stdout, "[OK] %s\n", msg) }

// Info ist eine neutrale Auskunft.
func (o *output) Info(msg string) { fmt.Fprintf(o.stdout, "[INFO] %s\n", msg) }

// Warn: nicht wie gedacht, es geht trotzdem weiter.
func (o *output) Warn(msg string) { fmt.Fprintf(o.stderr, "[WARNUNG] %s\n", msg) }

// Error: etwas ist gescheitert. Danach kommt, wo es einen gibt, ein Hint.
func (o *output) Error(msg string) { fmt.Fprintf(o.stderr, "[FEHLER] %s\n", msg) }

// Hint sagt, was der Nutzer tun kann, eingerückt unter dem Fehler.
func (o *output) Hint(msg string) { fmt.Fprintf(o.stderr, "        %s\n", msg) }
