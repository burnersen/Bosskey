// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Tests für tasten.json, die Übernahme aus 0.1.0 und das Ändern der Liste.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustKey(t *testing.T, s string) hotkey {
	t.Helper()
	h, err := parseHotkey(s)
	if err != nil {
		t.Fatal(s, err)
	}
	return h
}

func describe(list []binding) string {
	var parts []string
	for _, b := range list {
		parts = append(parts, b.Hotkey.String()+"="+targetText(b.Target))
	}
	return strings.Join(parts, " ")
}

func TestBindingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", bindingsFileName)
	list := []binding{
		{mustKey(t, "Strg+Alt+Z"), target{Back: true}},
		{mustKey(t, "Strg+Alt+2"), target{Desktop: 2}},
		{mustKey(t, "Pause"), target{Desktop: 1}},
	}
	if err := writeBindings(path, list); err != nil {
		t.Fatal(err)
	}
	got, err := readBindings(path)
	if err != nil || got.Missing || len(got.Warnings) > 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if d := describe(got.Bindings); d != "Pause=1 Strg+Alt+2=2 Strg+Alt+Z=zurück" {
		t.Fatalf("gelesen: %s", d)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"taste": "Strg+Alt+2"`) || !strings.Contains(string(data), `"format": 1`) {
		t.Fatalf("Datei: %s", data)
	}
}

func TestReadBindingsProblems(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Kaputte Einträge werden ausgelassen, der Rest gilt.
	p := write("a.json", `{"format":1,"tasten":[{"taste":"Pause","ziel":"1"},{"taste":"Strg+Blubb","ziel":"2"},{"taste":"F8","ziel":"null"},{"taste":"pause","ziel":"3"}]}`)
	got, err := readBindings(p)
	if err != nil || describe(got.Bindings) != "Pause=1" || len(got.Warnings) != 3 {
		t.Fatalf("%s %q %v", describe(got.Bindings), got.Warnings, err)
	}
	// Kaputte Datei: Fehler, nicht still überschreiben.
	if _, err := readBindings(write("b.json", `{"format":1,`)); err == nil {
		t.Fatal("kaputte Datei ohne Fehler")
	}
	// Neueres Format: Fehler.
	if _, err := readBindings(write("c.json", `{"format":2,"tasten":[]}`)); !errors.Is(err, errNewerFormat) {
		t.Fatalf("neueres Format: %v", err)
	}
	// Leere Liste ist erlaubt (alles entfernt).
	got, err = readBindings(write("d.json", `{"format":1,"tasten":[]}`))
	if err != nil || got.Missing || len(got.Bindings) != 0 {
		t.Fatalf("leer: %+v %v", got, err)
	}
}

func TestLoadBindingsFirstStartAndMigration(t *testing.T) {
	// Erster Start: Standard-Taste, Datei wird angelegt.
	path := filepath.Join(t.TempDir(), bindingsFileName)
	res, err := loadBindings(path, legacyHotkeys{})
	if err != nil || !res.Created || describe(res.Bindings) != "Pause=1" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("tasten.json nicht angelegt")
	}
	// Zweiter Start: liest die Datei, keine Übernahme, kein "erster Start".
	res, err = loadBindings(path, legacyHotkeys{Present: true, Bindings: []binding{{mustKey(t, "F9"), target{Desktop: 4}}}})
	if err != nil || res.Created || res.Migrated != 0 || describe(res.Bindings) != "Pause=1" {
		t.Fatalf("zweiter Start: %+v %v", res, err)
	}

	// Von 0.1.0: Tasten aus der INI.
	path = filepath.Join(t.TempDir(), bindingsFileName)
	legacy := legacyHotkeys{Present: true, Bindings: []binding{{mustKey(t, "F9"), target{Desktop: 4}}, {mustKey(t, "Pause"), target{Desktop: 1}}}}
	res, err = loadBindings(path, legacy)
	if err != nil || res.Created || res.Migrated != 2 || describe(res.Bindings) != "F9=4 Pause=1" {
		t.Fatalf("Übernahme: %+v %v", res, err)
	}
	again, _ := readBindings(path)
	if describe(again.Bindings) != "Pause=1 F9=4" {
		t.Fatalf("gespeichert: %s", describe(again.Bindings))
	}

	// 0.1.0 mit leerem [Tasten]: bleibt leer, keine Standard-Taste.
	path = filepath.Join(t.TempDir(), bindingsFileName)
	res, err = loadBindings(path, legacyHotkeys{Present: true})
	if err != nil || res.Created || len(res.Bindings) != 0 {
		t.Fatalf("leere Übernahme: %+v %v", res, err)
	}
}

func TestAssignBinding(t *testing.T) {
	pause, f9, z := mustKey(t, "Pause"), mustKey(t, "F9"), mustKey(t, "Strg+Alt+Z")
	list := []binding{{pause, target{Desktop: 1}}, {f9, target{Desktop: 3}}}

	// Neue Taste dazu, sortiert nach Ziel.
	got, before := assignBinding(list, z, target{Desktop: 2}, -1)
	if describe(got) != "Pause=1 Strg+Alt+Z=2 F9=3" || before != nil {
		t.Fatalf("dazu: %s %v", describe(got), before)
	}
	// Vorhandene Taste auf ein anderes Ziel umhängen.
	got, before = assignBinding(list, f9, target{Desktop: 2}, -1)
	if describe(got) != "Pause=1 F9=2" || before == nil || before.Desktop != 3 {
		t.Fatalf("umhängen: %s %v", describe(got), before)
	}
	// Eintrag 1 (F9 = 3) bekommt eine neue Taste.
	got, before = assignBinding(list, z, target{Desktop: 3}, 1)
	if describe(got) != "Pause=1 Strg+Alt+Z=3" || before != nil {
		t.Fatalf("neu aufnehmen: %s %v", describe(got), before)
	}
	// Neu aufnehmen mit einer Taste, die schon woanders hängt: die alte Zeile verschwindet.
	got, before = assignBinding(list, pause, target{Desktop: 3}, 1)
	if describe(got) != "Pause=3" || before == nil || before.Desktop != 1 {
		t.Fatalf("neu aufnehmen mit vorhandener Taste: %s %v", describe(got), before)
	}
	// Dieselbe Taste fürs selbe Ziel: nichts geändert.
	got, before = assignBinding(list, pause, target{Desktop: 1}, -1)
	if describe(got) != "Pause=1 F9=3" || before != nil {
		t.Fatalf("doppelt: %s %v", describe(got), before)
	}
	// Liste unverändert.
	if describe(list) != "Pause=1 F9=3" {
		t.Fatalf("Eingabe verändert: %s", describe(list))
	}
}

func TestRemoveAndNextFree(t *testing.T) {
	list := []binding{{mustKey(t, "Pause"), target{Desktop: 1}}, {mustKey(t, "F9"), target{Desktop: 2}}, {mustKey(t, "F10"), target{Back: true}}}
	if d := describe(removeBinding(list, 1)); d != "Pause=1 F10=zurück" {
		t.Fatal(d)
	}
	if d := describe(removeBinding(list, 7)); d != describe(list) {
		t.Fatal("Index außerhalb hat etwas verändert")
	}
	if n := nextFreeDesktop(list); n != 3 {
		t.Fatalf("nächster freier: %d", n)
	}
	if n := nextFreeDesktop(nil); n != 1 {
		t.Fatalf("leer: %d", n)
	}
}
