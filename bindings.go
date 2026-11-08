// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Die Tasten in tasten.json. Seit 0.2.0 richtet der Nutzer sie im Fenster
// "Tasten einrichten" ein, also schreibt nur noch das Programm sie: nach
// Kit-Regel K2 gehört so etwas in JSON, nicht in die INI des Nutzers.
// Version 0.1.0 hatte sie in der INI unter [Tasten]; die werden einmal
// übernommen (migrateBindings).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// bindingsFormat ist die Version des Dateiformats (Kit-Regel K10). Eine
// Datei mit höherer Nummer stammt von einer neueren Version und wird nie
// überschrieben.
const bindingsFormat = 1

const bindingsFileName = "tasten.json"

// bindingsPath: neben der INI, also unter %APPDATA% (wandert mit dem
// Benutzerprofil mit) bzw. im portablen Ordner.
func bindingsPath(p appPaths) string {
	return filepath.Join(filepath.Dir(p.ConfigFile), bindingsFileName)
}

type bindingsFile struct {
	Format   int            `json:"format"`
	Hinweis  string         `json:"hinweis,omitempty"`
	Bindings []bindingEntry `json:"tasten"`
}

// bindingEntry hält Taste und Ziel als Text, so wie sie im Fenster stehen.
// So bleibt die Datei lesbar, und die Codes von Windows stehen nur an
// einer Stelle (hotkey.go).
type bindingEntry struct {
	Key    string `json:"taste"`
	Target string `json:"ziel"`
}

const bindingsHint = "Wird von Bosskey geschrieben. Ändern über Rechtsklick aufs Symbol > Tasten einrichten."

// defaultBindings: die Boss-Taste, mit der Bosskey ab dem ersten Start
// etwas tut.
func defaultBindings() []binding {
	h, _ := parseHotkey("Pause") // ignore: fester Text, TestDefaultsAreValid prüft ihn
	return []binding{{Hotkey: h, Target: target{Desktop: 1}}}
}

var errNewerFormat = errors.New("von einer neueren Version")

// bindingsLoad ist, was beim Lesen von tasten.json herauskam.
type bindingsLoad struct {
	Bindings []binding
	Missing  bool     // Datei gibt es nicht
	Warnings []string // Einträge, die nicht gehen (werden ausgelassen)
}

// readBindings liest tasten.json. Kaputte Einträge werden ausgelassen und
// gemeldet, eine kaputte oder zu neue Datei ist ein Fehler: dann lieber
// nichts tun, als sie beim nächsten Speichern zu überschreiben.
func readBindings(path string) (bindingsLoad, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return bindingsLoad{Missing: true}, nil
	}
	if err != nil {
		return bindingsLoad{}, fileError(err, "lesen", path)
	}
	var f bindingsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return bindingsLoad{}, userError(err, "Kann "+path+" nicht lesen, die Datei ist beschädigt",
			"Lösch sie, dann legt Bosskey sie neu an (mit der Taste Pause für Desktop 1).")
	}
	if f.Format > bindingsFormat {
		return bindingsLoad{}, userError(errNewerFormat, path+" stammt von einer neueren Bosskey-Version",
			"Nimm die neuere Version. Diese hier ändert die Datei nicht.")
	}
	var res bindingsLoad
	seen := map[hotkey]bool{}
	for i, e := range f.Bindings {
		h, err := parseHotkey(e.Key)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s Eintrag %d (%s): %v. Ausgelassen.", bindingsFileName, i+1, e.Key, err))
			continue
		}
		t, err := parseTarget(e.Target)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s Eintrag %d (%s = %s): %v. Ausgelassen.", bindingsFileName, i+1, e.Key, e.Target, err))
			continue
		}
		if seen[h] {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s Eintrag %d: %s steht doppelt, ausgelassen.", bindingsFileName, i+1, h))
			continue
		}
		seen[h] = true
		res.Bindings = append(res.Bindings, binding{Hotkey: h, Target: t})
	}
	return res, nil
}

// writeBindings speichert die Tasten, sortiert, atomar.
func writeBindings(path string, list []binding) error {
	f := bindingsFile{Format: bindingsFormat, Hinweis: bindingsHint, Bindings: []bindingEntry{}}
	for _, b := range sortedBindings(list) {
		f.Bindings = append(f.Bindings, bindingEntry{Key: b.Hotkey.String(), Target: targetText(b.Target)})
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, append(data, '\n'), 0o644); err != nil {
		return fileError(err, "speichern", path)
	}
	return nil
}

// targetText ist das Ziel so, wie parseTarget es wieder liest.
func targetText(t target) string {
	if t.Back {
		return "zurück"
	}
	return strconv.Itoa(t.Desktop)
}

// bindingsResult sagt, woher die Tasten beim Start kamen.
type bindingsResult struct {
	Bindings []binding
	Created  bool // erster Start: Standard-Taste angelegt
	Migrated int  // so viele Tasten aus der INI (Version 0.1.0) übernommen
	Warnings []string
}

// loadBindings liest tasten.json. Fehlt sie, kommen die Tasten aus dem
// alten Abschnitt [Tasten] der INI, sonst die Standard-Taste, und die
// Datei wird angelegt.
func loadBindings(path string, legacy legacyHotkeys) (bindingsResult, error) {
	got, err := readBindings(path)
	if err != nil {
		return bindingsResult{}, err
	}
	if !got.Missing {
		return bindingsResult{Bindings: got.Bindings, Warnings: got.Warnings}, nil
	}
	res := migrateBindings(legacy)
	if err := writeBindings(path, res.Bindings); err != nil {
		// Die Tasten gelten trotzdem, nur gespeichert sind sie nicht.
		res.Warnings = append(res.Warnings, fmt.Sprintf("Kann die Tasten nicht speichern (%v).", err))
	}
	return res, nil
}

// migrateBindings entscheidet, womit es ohne tasten.json losgeht.
func migrateBindings(legacy legacyHotkeys) bindingsResult {
	if legacy.Present {
		return bindingsResult{Bindings: legacy.Bindings, Migrated: len(legacy.Bindings), Warnings: legacy.Warnings}
	}
	return bindingsResult{Bindings: defaultBindings(), Created: true}
}

// sortedBindings: erst die Desktops aufsteigend, "zurück" am Ende; bei
// gleichem Ziel in der Reihenfolge, in der sie dazukamen.
func sortedBindings(list []binding) []binding {
	out := append([]binding(nil), list...)
	sort.SliceStable(out, func(i, j int) bool { return targetOrder(out[i].Target) < targetOrder(out[j].Target) })
	return out
}

func targetOrder(t target) int {
	if t.Back {
		return maxDesktop + 1
	}
	return t.Desktop
}

// assignBinding legt eine Taste für ein Ziel fest.
//
//	replace  Index des Eintrags, dessen Taste neu aufgenommen wurde, sonst -1
//
// Hatte die Taste schon ein anderes Ziel, wird sie umgehängt (eine Taste,
// ein Ziel); before sagt dann, welches es vorher war.
func assignBinding(list []binding, h hotkey, t target, replace int) (out []binding, before *target) {
	for i, b := range list {
		switch {
		case i == replace:
			// dieser Eintrag bekommt die neue Taste
		case b.Hotkey == h:
			old := b.Target
			before = &old
		default:
			out = append(out, b)
		}
	}
	out = append(out, binding{Hotkey: h, Target: t})
	if before != nil && *before == t {
		before = nil // dieselbe Taste fürs selbe Ziel nochmal: nichts geändert
	}
	return sortedBindings(out), before
}

// removeBinding nimmt den Eintrag i heraus.
func removeBinding(list []binding, i int) []binding {
	if i < 0 || i >= len(list) {
		return list
	}
	out := append([]binding(nil), list[:i]...)
	return append(out, list[i+1:]...)
}

// nextFreeDesktop ist der kleinste Desktop ohne Taste, für die Vorauswahl
// im Fenster.
func nextFreeDesktop(list []binding) int {
	used := map[int]bool{}
	for _, b := range list {
		used[b.Target.Desktop] = true
	}
	for d := 1; d <= maxDesktop; d++ {
		if !used[d] {
			return d
		}
	}
	return 1
}
