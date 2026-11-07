// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Tests für das Rechnen mit Desktops.
package main

import (
	"bytes"
	"errors"
	"testing"
)

func ids(n int) []byte {
	var b []byte
	for i := range n {
		b = append(b, bytes.Repeat([]byte{byte(i + 1)}, guidSize)...)
	}
	return b
}

func TestDesktopPosition(t *testing.T) {
	all := ids(4)
	cases := []struct {
		name     string
		ids, cur []byte
		pos, cnt int
		err      error
	}{
		{"keine Liste", nil, nil, 0, 1, nil},
		{"ein Desktop", ids(1), nil, 0, 1, nil},
		{"dritter von vier", all, all[2*guidSize : 3*guidSize], 2, 4, nil},
		{"erster", all, all[:guidSize], 0, 4, nil},
		{"aktueller fehlt", all, nil, 0, 4, errCurrentUnknown},
		{"aktueller unbekannt", all, bytes.Repeat([]byte{9}, guidSize), 0, 4, errCurrentUnknown},
	}
	for _, c := range cases {
		pos, cnt, err := desktopPosition(c.ids, c.cur)
		if pos != c.pos || cnt != c.cnt || !errors.Is(err, c.err) {
			t.Errorf("%s: %d %d %v, erwartet %d %d %v", c.name, pos, cnt, err, c.pos, c.cnt, c.err)
		}
	}
	if _, _, err := desktopPosition(make([]byte, 17), nil); err == nil {
		t.Error("kaputte Liste sollte ein Fehler sein")
	}
}

func TestParseTarget(t *testing.T) {
	for in, want := range map[string]target{"1": {Desktop: 1}, " 12 ": {Desktop: 12}, "zurück": {Back: true}, "Zurueck": {Back: true}, "back": {Back: true}} {
		got, err := parseTarget(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	for _, in := range []string{"0", "-1", "100", "eins", ""} {
		if _, err := parseTarget(in); err == nil {
			t.Errorf("%q sollte nicht gehen", in)
		}
	}
}

func TestDecideDestination(t *testing.T) {
	d1 := target{Desktop: 1}
	back := target{Back: true}
	cases := []struct {
		name          string
		t             target
		current, prev int
		toggle        bool
		want          int
		wantErr       bool
	}{
		{"Boss-Taste von 3", d1, 2, noPrevious, false, 0, false},
		{"schon auf 1, ohne Zurück", d1, 0, 2, false, 0, false},
		{"schon auf 1, mit Zurück", d1, 0, 2, true, 2, false},
		{"schon auf 1, mit Zurück, kein vorher", d1, 0, noPrevious, true, 0, false},
		{"zurück", back, 0, 3, false, 3, false},
		{"zurück ohne vorher", back, 0, noPrevious, false, 0, true},
		{"aktueller unbekannt", target{Desktop: 2}, noPrevious, noPrevious, true, 1, false},
	}
	for _, c := range cases {
		got, err := decideDestination(c.t, c.current, c.prev, c.toggle)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("%s: %d %v, erwartet %d Fehler=%v", c.name, got, err, c.want, c.wantErr)
		}
	}
}
