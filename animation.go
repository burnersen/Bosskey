// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Desktop-Wechsel ohne Animation: Bosskey schaltet zwei Animationen von
// Windows ab und merkt sich, wie sie vorher standen, damit der Haken im
// Menü alles genau so wiederherstellen kann. Hier nur Zustand und Datei,
// ohne Windows; das Lesen und Setzen steht in animation_windows.go.
//
// Welche Schalter den Wechsel steuern (Stand 08.11.2026, aus Berichten,
// nicht selbst gemessen):
//   - Windows 11: "Steuerelemente und Elemente innerhalb von Fenstern
//     animieren" (SPI_SETCLIENTAREAANIMATION), in den Einstellungen der
//     Schalter Barrierefreiheit > Visuelle Effekte > Animationseffekte
//   - Windows 10: "Animation beim Minimieren und Maximieren von Fenstern"
//     (SPI_SETANIMATION)
//
// Beide sind Einstellungen des Benutzers, ohne Adminrechte. Bosskey setzt
// immer beide, das ist einfacher und unter beiden Windows richtig.
package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const animationFileName = "animation.json"

func animationPath(p appPaths) string {
	return filepath.Join(filepath.Dir(p.ConfigFile), animationFileName)
}

// animations ist der Stand der beiden Schalter.
type animations struct {
	ClientArea bool `json:"steuerelemente"` // Windows 11: Animationseffekte
	MinMax     bool `json:"fenster"`        // Windows 10: Minimieren und Maximieren
}

func (a animations) anyOn() bool { return a.ClientArea || a.MinMax }

// animationState ist animation.json. Programmzustand, darum JSON (Kit K2).
type animationState struct {
	Format int    `json:"format"`
	Hint   string `json:"hinweis,omitempty"`
	// Hide: der Haken "Desktop-Wechsel ohne Animation". Ab Werk an.
	Hide bool `json:"ohneAnimation"`
	// Before: so standen die Schalter, bevor Bosskey sie abgeschaltet hat.
	// nil heißt: Bosskey hat nichts verändert, es gibt nichts zurückzustellen.
	Before *animations `json:"vorher,omitempty"`
}

const animationFormat = 1

func defaultAnimationState() animationState {
	return animationState{Format: animationFormat, Hide: true}
}

// readAnimationState liest animation.json; fehlt sie, gilt der Standard.
// Eine kaputte oder zu neue Datei ist ein Fehler: dann fasst Bosskey die
// Animationen nicht an, statt sich ein falsches "vorher" zu merken.
func readAnimationState(path string) (animationState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return defaultAnimationState(), nil
	}
	if err != nil {
		return animationState{}, err
	}
	var s animationState
	if err := json.Unmarshal(data, &s); err != nil {
		return animationState{}, err
	}
	if s.Format > animationFormat {
		return animationState{}, errNewerFormat
	}
	return s, nil
}

func writeAnimationState(path string, s animationState) error {
	s.Format = animationFormat
	s.Hint = "Wird von Bosskey geschrieben. Ändern über Rechtsklick aufs Symbol > Desktop-Wechsel ohne Animation."
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

// animationPlan sagt, was zu tun ist.
type animationPlan struct {
	Set     bool       // Schalter setzen
	To      animations // auf diesen Stand
	State   animationState
	Changed bool // es ändert sich etwas, das der Nutzer erfahren soll
}

// planHide: der Haken ist an (Start oder gerade angeklickt). Sind noch
// Animationen an, werden sie abgeschaltet; das "vorher" wird nur beim
// ersten Mal festgehalten, damit ein zweites Abschalten (der Nutzer hat sie
// in Windows wieder angemacht) nicht den Stand von Bosskey als "vorher" merkt.
func planHide(s animationState, current animations) animationPlan {
	s.Hide = true
	if !current.anyOn() {
		return animationPlan{State: s}
	}
	if s.Before == nil {
		before := current
		s.Before = &before
	}
	return animationPlan{Set: true, To: animations{}, State: s, Changed: true}
}

// planRestore: der Haken wird herausgenommen. Was Bosskey abgeschaltet
// hat, kommt zurück; hatte es nichts verändert, bleibt alles, wie es ist.
func planRestore(s animationState) animationPlan {
	s.Hide = false
	if s.Before == nil {
		return animationPlan{State: s}
	}
	to := *s.Before
	s.Before = nil
	return animationPlan{Set: true, To: to, State: s, Changed: true}
}

// animationTip steht in der Meldung, wenn das Abschalten nicht klappt. Kurz,
// weil die Sprechblase nur 255 Zeichen hat; Windows 10 steht im README.
const animationTip = "Tipp: Windows-Einstellungen > Barrierefreiheit > Visuelle Effekte > Animationseffekte aus."
