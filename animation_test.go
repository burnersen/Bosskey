// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Tests für "Desktop-Wechsel ohne Animation": was abgeschaltet und was
// wiederhergestellt wird.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAnimationPlans(t *testing.T) {
	on := animations{ClientArea: true, MinMax: true}
	mixed := animations{ClientArea: false, MinMax: true}

	// Erster Start, Animationen an: aus, "vorher" merken.
	p := planHide(defaultAnimationState(), mixed)
	if !p.Set || p.To.anyOn() || !p.Changed || p.State.Before == nil || *p.State.Before != mixed {
		t.Fatalf("abschalten: %+v", p)
	}
	// Nutzer schaltet in Windows wieder an, Bosskey startet neu: aus, aber
	// das ursprüngliche "vorher" bleibt.
	p2 := planHide(p.State, on)
	if !p2.Set || *p2.State.Before != mixed {
		t.Fatalf("zweites Abschalten hat vorher überschrieben: %+v", p2.State.Before)
	}
	// Schon aus: nichts tun, nichts melden.
	p3 := planHide(p2.State, animations{})
	if p3.Set || p3.Changed {
		t.Fatalf("schon aus: %+v", p3)
	}
	// Haken raus: genau der alte Stand kommt zurück.
	r := planRestore(p3.State)
	if !r.Set || r.To != mixed || r.State.Hide || r.State.Before != nil {
		t.Fatalf("zurück: %+v", r)
	}
	// Haken raus, ohne dass Bosskey etwas verändert hatte: nichts anfassen.
	r2 := planRestore(planHide(defaultAnimationState(), animations{}).State)
	if r2.Set {
		t.Fatalf("zurück ohne vorher: %+v", r2)
	}
}

func TestAnimationStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, animationFileName)
	s, err := readAnimationState(path)
	if err != nil || !s.Hide || s.Before != nil {
		t.Fatalf("Standard: %+v %v", s, err)
	}
	s.Before = &animations{ClientArea: true}
	if err := writeAnimationState(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := readAnimationState(path)
	if err != nil || !got.Hide || got.Before == nil || !got.Before.ClientArea || got.Before.MinMax {
		t.Fatalf("gelesen: %+v %v", got, err)
	}
	if err := os.WriteFile(path, []byte(`{"format":9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAnimationState(path); !errors.Is(err, errNewerFormat) {
		t.Fatalf("neueres Format: %v", err)
	}
}
