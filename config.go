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

// config ist alles, was der Nutzer in der INI einstellen kann.
type config struct {
	ShowStartMessage bool
	ToggleBack       bool
	Bindings         []binding // aus dem Abschnitt [Tasten], in der Reihenfolge der Datei
}

// binding ist eine Zeile aus [Tasten]: diese Taste springt dorthin.
type binding struct {
	Hotkey hotkey
	Target target
	Line   int // Zeile in der Datei, für Meldungen
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

// hotkeySection ist der Abschnitt mit den Tasten. Andere Schreibweisen
// werden auch erkannt (sectionNames).
const hotkeySection = "Tasten"

var sectionNames = []string{"tasten", "tastenkuerzel", "hotkeys"}

// hotkeyHelp steht in einer neuen Datei unter [Tasten].
var hotkeyHelp = []string{
	"Eine Zeile je Taste:  Taste = Desktop-Nummer  (1 ist der erste, ganz links in der Übersicht mit Win+Tab).",
	"Statt einer Nummer geht auch \"zurück\": dann springt die Taste dorthin, wo du vor dem letzten Sprung warst.",
	"Umschalttasten: Strg, Alt, Umschalt, Win, AltGr. Beispiele:",
	"  Strg+Alt+1 = 1",
	"  Strg+Alt+2 = 2",
	"  F12 = 1",
	"  Strg+Alt+Z = zurück",
	"Tasten mit Namen: Pause, Rollen, Druck, Einfg, Entf, Pos1, Ende, BildAuf, BildAb, Menue, Leertaste,",
	"Esc, Tab, Enter, Links, Hoch, Rechts, Runter, F1 bis F24, Num0 bis Num9, A bis Z, 0 bis 9.",
	"Gut als Boss-Taste: eine, die du sonst nie brauchst, z. B. Pause, Rollen oder Menue.",
}

// defaultBinding steht in einer neuen Datei aktiv unter [Tasten]: die
// Boss-Taste, mit der Bosskey gleich etwas tut.
const defaultBinding = "Pause = 1"

// configLoad ist, was beim Lesen herauskam. Created, Added und Warnings
// meldet der Aufrufer dem Nutzer.
type configLoad struct {
	Config   config
	Created  bool     // Datei fehlte und wurde mit Standardwerten angelegt
	Missing  bool     // Datei fehlt (nur readConfig, das nichts anlegt)
	Added    []string // fehlende Schlüssel, die angehängt wurden (alte Datei als .bak daneben)
	Warnings []string // unbekannte Schlüssel, ungültige Werte, Tasten, die nicht gehen
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
		cfg, _, warnings := parseConfig(filepath.Base(path), configTemplate())
		return parsed{configLoad: configLoad{Config: cfg, Missing: true, Warnings: warnings}}, nil, nil
	}
	if err != nil {
		return parsed{}, nil, fileError(err, "lesen", path)
	}
	cfg, seen, warnings := parseConfig(filepath.Base(path), string(data))
	return parsed{configLoad: configLoad{Config: cfg, Warnings: warnings}, seen: seen}, data, nil
}

// parseConfig liest den Text der INI. name steht in den Warnungen.
//
// Schlüssel aus der Tabelle gelten überall in der Datei, auch unter
// [Tasten]: neue Schlüssel werden ans Ende angehängt, und das Ende ist
// meistens der Abschnitt [Tasten]. Alles andere unter [Tasten] ist eine Taste.
func parseConfig(name, text string) (config, map[string]bool, []string) {
	cfg := defaultConfig()
	seen := map[string]bool{}
	var warnings []string
	warn := func(line int, format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf("%s Zeile %d: ", name, line)+fmt.Sprintf(format, args...))
	}
	usedAt := map[hotkey]int{}
	inHotkeys := false

	text = strings.TrimPrefix(text, "\ufeff") // Windows-Editoren setzen gern ein BOM davor
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			inHotkeys = isHotkeySection(section)
			if !inHotkeys {
				warn(n, "unbekannter Abschnitt [%s], die Zeilen darunter werden übergangen (gemeint war wohl [%s]).", section, hotkeySection)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok {
			warn(n, "%q ist nicht Schlüssel = Wert, übergangen.", line)
			continue
		}
		if s, known := findSetting(key); known {
			seen[s.key] = true
			if err := s.apply(&cfg, value); err != nil {
				_ = s.apply(&cfg, s.def) // ignore: Standardwerte prüft TestDefaultsAreValid
				warn(n, "%s=%s geht nicht (%v). Es gilt %s.", s.key, value, err, s.def)
			}
			continue
		}
		if !inHotkeys {
			if _, err := parseHotkey(key); err == nil {
				warn(n, "%s steht nicht unter [%s], übergangen. Tasten gehören in den Abschnitt [%s].", key, hotkeySection, hotkeySection)
			} else {
				warn(n, "unbekannte Einstellung %q, übergangen.", key)
			}
			continue
		}
		hk, err := parseHotkey(key)
		if err != nil {
			warn(n, "%s: %v. Übergangen.", key, err)
			continue
		}
		tg, err := parseTarget(value)
		if err != nil {
			warn(n, "%s = %s: %v. Übergangen.", key, value, err)
			continue
		}
		if first, dup := usedAt[hk]; dup {
			warn(n, "%s steht schon in Zeile %d, die zweite Zeile wird übergangen.", hk, first)
			continue
		}
		if hk.blocksTyping() {
			warn(n, "%s ohne Strg, Alt oder Win fehlt dir danach beim Schreiben. Gilt trotzdem.", hk)
		}
		usedAt[hk] = n
		cfg.Bindings = append(cfg.Bindings, binding{Hotkey: hk, Target: tg, Line: n})
	}
	return cfg, seen, warnings
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
	b.WriteString(configEntries(settings))
	b.WriteString("\n[" + hotkeySection + "]\n")
	for _, l := range hotkeyHelp {
		b.WriteString("# " + l + "\n")
	}
	b.WriteString(defaultBinding + "\n")
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
