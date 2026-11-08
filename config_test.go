// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Tests für die Einstellungsdatei: Regeln K1 bis K6 des Kits und der
// Abschnitt [Tasten].
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAreValid(t *testing.T) {
	for _, s := range settings {
		var c config
		if err := s.apply(&c, s.def); err != nil {
			t.Errorf("Standard von %s geht nicht: %v", s.key, err)
		}
		if strings.Contains(s.comment, "\n") {
			t.Errorf("Kommentar von %s hat einen Zeilenumbruch", s.key)
		}
	}
	if d := defaultBindings(); len(d) != 1 || d[0].Hotkey.VK != vkPause || d[0].Hotkey.Mods != 0 {
		t.Errorf("Standard-Taste: %+v", d)
	}
}

func TestMissingFileIsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.ini")
	res, err := loadConfig(path)
	if err != nil || !res.Created {
		t.Fatalf("created %v, err %v", res.Created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range settings {
		if !strings.Contains(string(data), s.key+"="+s.def) {
			t.Errorf("neue Datei ohne %s", s.key)
		}
	}
	if strings.Contains(string(data), "[Tasten]") || res.Legacy.Present {
		t.Fatal("neue Datei hat noch einen Abschnitt [Tasten]")
	}
	// Die neue Datei nochmal lesen: keine Warnung, nichts angehängt.
	again, err := loadConfig(path)
	if err != nil || again.Created || len(again.Added) > 0 || len(again.Warnings) > 0 {
		t.Fatalf("zweites Lesen: %+v, %v", again, err)
	}
}

func TestReadConfigWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.ini")
	res, err := readConfig(path)
	if err != nil || !res.Missing {
		t.Fatalf("%+v, %v", res, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("readConfig hat eine Datei angelegt")
	}
	path = writeConfig(t, "meldungBeimStart=false\n")
	if _, err := readConfig(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "meldungBeimStart=false\n" {
		t.Fatalf("readConfig hat angehängt: %q", data)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Fatal("readConfig hat eine Sicherung angelegt")
	}
}

func TestInvalidValueWarnsAndUsesDefault(t *testing.T) {
	path := writeConfig(t, "meldungBeimStart=vielleicht\nnochmalZurueck=ja\n")
	res, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Config.ShowStartMessage || !res.Config.ToggleBack {
		t.Fatalf("Werte: %+v", res.Config)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "Zeile 1") || !strings.Contains(res.Warnings[0], "Es gilt true") {
		t.Fatalf("Warnungen: %q", res.Warnings)
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "meldungBeimStart=vielleicht\nnochmalZurueck=ja\n") {
		t.Fatalf("Zeilen des Nutzers geändert: %q", data)
	}
}

func TestUnknownKeyWarns(t *testing.T) {
	path := writeConfig(t, "meldungBeimStart=false\nnochmalZurueck=false\nfarbe=blau\n")
	res, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "farbe") {
		t.Fatalf("Warnungen: %q", res.Warnings)
	}
}

func TestMissingKeysAreAppendedWithBackup(t *testing.T) {
	old := "\ufeff# meins\r\nmeldungBeimStart=false\r\n[Tasten]\r\nF12 = 2\r\n"
	path := writeConfig(t, old)
	res, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 1 || res.Added[0] != "nochmalZurueck" {
		t.Fatalf("angehängt: %q", res.Added)
	}
	if res.Config.ShowStartMessage {
		t.Fatal("BOM hat den ersten Schlüssel kaputt gemacht")
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || string(backup) != old {
		t.Fatalf("Sicherung %q, %v", backup, err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.HasPrefix(text, old) || !strings.Contains(text, "nochmalZurueck=false\r\n") {
		t.Fatalf("Datei danach: %q", text)
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatalf("gemischte Zeilenenden: %q", text)
	}
	// Der angehängte Schlüssel steht jetzt unter dem alten [Tasten] und muss
	// trotzdem als Einstellung gelten, nicht als Taste.
	again, err := loadConfig(path)
	if err != nil || len(again.Warnings) > 0 || len(again.Added) > 0 {
		t.Fatalf("nach dem Anhängen: %+v, %v", again, err)
	}
	if len(again.Legacy.Bindings) != 1 || again.Legacy.Bindings[0].Target.Desktop != 2 {
		t.Fatalf("alte Tasten: %+v", again.Legacy.Bindings)
	}
}

func TestLegacyHotkeySection(t *testing.T) {
	text := strings.Join([]string{
		"meldungBeimStart=true",
		"nochmalZurueck=false",
		"Strg+Alt+1 = 1",   // 3: außerhalb von [Tasten]: unbekannte Einstellung
		"[Tasten]",         // 4
		"Pause = 1",        // 5
		"Strg+Alt+2 = 2",   // 6
		"strg + alt + 2=3", // 7: doppelt
		"Strg+Hyper+3 = 3", // 8: unbekannte Umschalttaste
		"F5 = null",        // 9: kein Ziel
		"Win+Z = zurück",   // 10
		"A = 1",            // 11
		"[Sonstiges]",      // 12: unbekannter Abschnitt
		"F7 = 1",           // 13: darunter, schon mit dem Abschnitt gemeldet
	}, "\n")
	_, _, warnings, legacy := parseConfig("x.ini", text)
	if !legacy.Present {
		t.Fatal("[Tasten] nicht erkannt")
	}
	var got []string
	for _, b := range legacy.Bindings {
		got = append(got, b.Hotkey.String()+"="+b.Target.String())
	}
	want := []string{"Pause=Desktop 1", "Strg+Alt+2=Desktop 2", "Win+Z=zurück zum vorigen Desktop", "A=Desktop 1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Tasten:\n got %q\nwant %q", got, want)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], "Zeile 3:") || !strings.Contains(warnings[1], "Zeile 12:") {
		t.Errorf("Warnungen der INI: %q", warnings)
	}
	lw := strings.Join(legacy.Warnings, "\n")
	for _, w := range []string{"Zeile 7:", "Zeile 8:", "Zeile 9:"} {
		if !strings.Contains(lw, w) {
			t.Errorf("keine Warnung %q in %q", w, legacy.Warnings)
		}
	}
	if len(legacy.Warnings) != 3 {
		t.Errorf("%d Warnungen zu alten Tasten, erwartet 3: %q", len(legacy.Warnings), legacy.Warnings)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.ini")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
