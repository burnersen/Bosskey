// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Die Einstellungsdatei (INI). Sie gehört dem Nutzer: Bosskey liest sie,
// legt sie an, wenn sie fehlt, hängt neue Schlüssel höchstens ans Ende an
// und ändert nie eine Zeile, die der Nutzer geschrieben hat.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// config ist alles, was der Nutzer in der INI einstellen kann. Die Tasten
// stehen seit 0.2.0 in tasten.json (bindings.go).
type config struct {
	ShowStartMessage bool
	ToggleBack       bool
}

// binding ist eine Taste und ihr Ziel.
type binding struct {
	Hotkey hotkey
	Target target
}

// legacyHotkeys ist der Abschnitt [Tasten] aus Version 0.1.0. Er wird nur
// gelesen, um die Tasten einmal nach tasten.json zu übernehmen; danach
// übergeht Bosskey ihn still (wie ein ausgemusterter Schlüssel, Kit K5).
type legacyHotkeys struct {
	Present  bool
	Bindings []binding
	Warnings []string
}

// setting ist ein Schlüssel der INI außerhalb von [Tasten]. Diese Tabelle
// ist die einzige Stelle, an der ein Schlüssel beschrieben wird: Standard,
// Kommentar in einer neuen Datei und Prüfung kommen alle von hier.
type setting struct {
	key     string
	def     string
	comment string // steht über dem Schlüssel in der Datei, eine Zeile
	apply   func(c *config, value string) error
}

var settings = []setting{
	{
		key:     "meldungBeimStart",
		def:     "true",
		comment: "Beim Start kurz anzeigen, welche Tasten aktiv sind (true oder false).",
		apply: func(c *config, v string) (err error) {
			c.ShowStartMessage, err = parseBool(v)
			return err
		},
	},
	{
		key: "nochmalZurueck",
		def: "false",
		comment: "Nochmal drücken, obwohl du schon auf dem Ziel-Desktop bist, springt zurück dorthin, wo du vorher warst (true oder false). " +
			"Aus, damit ein doppelter Druck vor dem Chef nicht alles wieder aufdeckt.",
		apply: func(c *config, v string) (err error) {
			c.ToggleBack, err = parseBool(v)
			return err
		},
	},
}

// sectionNames: so darf der Abschnitt mit den Tasten aus Version 0.1.0
// heißen ([Tasten] und zwei andere Schreibweisen).
var sectionNames = []string{"tasten", "tastenkuerzel", "hotkeys"}

// configLoad ist, was beim Lesen herauskam. Created, Added und Warnings
// meldet der Aufrufer dem Nutzer.
type configLoad struct {
	Config   config
	Created  bool     // Datei fehlte und wurde mit Standardwerten angelegt
	Missing  bool     // Datei fehlt (nur readConfig, das nichts anlegt)
	Added    []string // fehlende Schlüssel, die angehängt wurden (alte Datei als .bak daneben)
	Warnings []string // unbekannte Schlüssel, ungültige Werte
	Legacy   legacyHotkeys
}

// loadConfig liest die INI für den Tray-Betrieb.
//
//   - Fehlt die Datei: wird mit allen Standardwerten und Kommentaren angelegt.
//   - Ungültiger Wert: Warnung mit Zeile, Schlüssel und dem Standard, der
//     stattdessen gilt. Die Zeile des Nutzers bleibt, wie sie ist.
//   - Unbekannter Schlüssel, falsche Taste: Warnung, sonst nichts.
//   - Fehlende Schlüssel (eine neuere Version hat welche dazubekommen):
//     werden ans Ende angehängt, vorher wird die alte Datei als .bak gesichert.
func loadConfig(path string) (configLoad, error) {
	res, data, err := readConfigData(path)
	if err != nil {
		return res.configLoad, err
	}
	if res.Missing {
		res.Missing = false
		if err := writeFileAtomic(path, []byte(configTemplate()), 0o644); err != nil {
			// Die Standardwerte gehen trotzdem, nur ändern kann man sie noch nicht.
			res.Warnings = append(res.Warnings, fmt.Sprintf("Kann die Einstellungsdatei %s nicht anlegen (%v). Es gelten die Standardwerte.", path, err))
			return res.configLoad, nil
		}
		res.Created = true
		return res.configLoad, nil
	}

	missing := missingSettings(res.seen)
	if len(missing) > 0 {
		if err := appendMissing(path, data, missing); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Kann neue Einstellungen nicht an %s anhängen (%v). Es gelten ihre Standardwerte.", path, err))
		} else {
			for _, s := range missing {
				res.Added = append(res.Added, s.key)
			}
		}
	}
	return res.configLoad, nil
}

// readConfig liest die INI, ohne irgendetwas zu schreiben (-pruefen).
func readConfig(path string) (configLoad, error) {
	res, _, err := readConfigData(path)
	return res.configLoad, err
}

// parsed ist configLoad plus, welche Schlüssel in der Datei standen.
type parsed struct {
	configLoad
	seen map[string]bool
}

func readConfigData(path string) (parsed, []byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg, _, warnings, _ := parseConfig(filepath.Base(path), configTemplate())
		return parsed{configLoad: configLoad{Config: cfg, Missing: true, Warnings: warnings}}, nil, nil
	}
	if err != nil {
		return parsed{}, nil, fileError(err, "lesen", path)
	}
	cfg, seen, warnings, legacy := parseConfig(filepath.Base(path), string(data))
	return parsed{configLoad: configLoad{Config: cfg, Warnings: warnings, Legacy: legacy}, seen: seen}, data, nil
}

// parseConfig liest den Text der INI. name steht in den Warnungen.
//
// Schlüssel aus der Tabelle gelten überall in der Datei, auch unter einem
// alten [Tasten]: neue Schlüssel werden ans Ende angehängt, und das Ende
// ist bei Dateien aus 0.1.0 der Abschnitt [Tasten]. Alles andere unter
// [Tasten] ist eine Taste aus 0.1.0 und landet in legacy.
func parseConfig(name, text string) (config, map[string]bool, []string, legacyHotkeys) {
	cfg := defaultConfig()
	seen := map[string]bool{}
	var warnings []string
	var legacy legacyHotkeys
	at := func(line int, format string, args ...any) string {
		return fmt.Sprintf("%s Zeile %d: ", name, line) + fmt.Sprintf(format, args...)
	}
	usedAt := map[hotkey]int{}
	inLegacy, inUnknown := false, false

	text = strings.TrimPrefix(text, "\ufeff") // Windows-Editoren setzen gern ein BOM davor
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			inLegacy = isHotkeySection(section)
			inUnknown = !inLegacy
			if inLegacy {
				legacy.Present = true
			} else {
				warnings = append(warnings, at(n, "unbekannter Abschnitt [%s], die Zeilen darunter werden übergangen.", section))
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok {
			warnings = append(warnings, at(n, "%q ist nicht Schlüssel = Wert, übergangen.", line))
			continue
		}
		if s, known := findSetting(key); known {
			seen[s.key] = true
			if err := s.apply(&cfg, value); err != nil {
				_ = s.apply(&cfg, s.def) // ignore: Standardwerte prüft TestDefaultsAreValid
				warnings = append(warnings, at(n, "%s=%s geht nicht (%v). Es gilt %s.", s.key, value, err, s.def))
			}
			continue
		}
		switch {
		case inUnknown:
			// schon beim Abschnitt gemeldet
		case !inLegacy:
			warnings = append(warnings, at(n, "unbekannte Einstellung %q, übergangen. Tasten richtest du über Rechtsklick aufs Symbol > Tasten einrichten ein.", key))
		default:
			legacy.add(key, value, n, usedAt, func(format string, args ...any) {
				legacy.Warnings = append(legacy.Warnings, at(n, format, args...))
			})
		}
	}
	return cfg, seen, warnings, legacy
}

// add liest eine Tastenzeile aus 0.1.0.
func (l *legacyHotkeys) add(key, value string, line int, usedAt map[hotkey]int, warn func(string, ...any)) {
	hk, err := parseHotkey(key)
	if err != nil {
		warn("%s: %v. Übergangen.", key, err)
		return
	}
	tg, err := parseTarget(value)
	if err != nil {
		warn("%s = %s: %v. Übergangen.", key, value, err)
		return
	}
	if first, dup := usedAt[hk]; dup {
		warn("%s steht schon in Zeile %d, die zweite Zeile wird übergangen.", hk, first)
		return
	}
	usedAt[hk] = line
	l.Bindings = append(l.Bindings, binding{Hotkey: hk, Target: tg})
}

func isHotkeySection(name string) bool {
	n := normalizeKeyText(name)
	for _, s := range sectionNames {
		if n == s {
			return true
		}
	}
	return false
}

func defaultConfig() config {
	var c config
	for _, s := range settings {
		_ = s.apply(&c, s.def) // ignore: Standardwerte prüft TestDefaultsAreValid
	}
	return c
}

func findSetting(key string) (setting, bool) {
	for _, s := range settings {
		if strings.EqualFold(s.key, key) {
			return s, true
		}
	}
	return setting{}, false
}

func missingSettings(seen map[string]bool) []setting {
	var missing []setting
	for _, s := range settings {
		if !seen[s.key] {
			missing = append(missing, s)
		}
	}
	return missing
}

// configTemplate ist eine komplette neue Einstellungsdatei.
func configTemplate() string {
	var b strings.Builder
	b.WriteString("# " + appName + ": Einstellungen\n")
	b.WriteString("# Zeilen mit # sind Kommentare. Eine Zeile löschen bringt ihren Standardwert zurück.\n")
	b.WriteString("# Gespeicherte Änderungen gelten nach ein paar Sekunden, ohne Neustart.\n")
	b.WriteString("# Die Tasten stehen nicht hier: Rechtsklick aufs Symbol > Tasten einrichten.\n")
	b.WriteString(configEntries(settings))
	return b.String()
}

// configEntries ist ein Block je Schlüssel: Leerzeile, Kommentar, key=Standard.
func configEntries(list []setting) string {
	var b strings.Builder
	for _, s := range list {
		fmt.Fprintf(&b, "\n# %s\n%s=%s\n", s.comment, s.key, s.def)
	}
	return b.String()
}

// appendMissing sichert die alte Datei und hängt die fehlenden Schlüssel an,
// mit den Zeilenenden, die die Datei schon hat.
func appendMissing(path string, old []byte, missing []setting) error {
	if err := writeFileAtomic(path+".bak", old, 0o644); err != nil {
		return fmt.Errorf("Sicherung anlegen: %w", err)
	}
	add := "\n# Von " + appName + " " + version + " ergänzt:\n" + configEntries(missing)
	if strings.Contains(string(old), "\r\n") {
		add = strings.ReplaceAll(add, "\n", "\r\n")
	}
	text := string(old)
	if !strings.HasSuffix(text, "\n") && text != "" {
		add = "\n" + add
	}
	return writeFileAtomic(path, []byte(text+add), 0o644)
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "true", "yes", "on", "1", "ja", "an", "wahr":
		return true, nil
	case "false", "no", "off", "0", "nein", "aus", "falsch":
		return false, nil
	}
	return false, errors.New("erlaubt sind true oder false")
}
