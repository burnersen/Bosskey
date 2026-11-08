// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Tests für das Lesen und Schreiben von Tastenkürzeln.
package main

import "testing"

func TestParseHotkey(t *testing.T) {
	cases := []struct {
		in   string
		mods uint32
		vk   uint32
		out  string
	}{
		{"Pause", 0, 0x13, "Pause"},
		{"pause", 0, 0x13, "Pause"},
		{"Strg+Alt+1", modControl | modAlt, 0x31, "Strg+Alt+1"},
		{"alt + strg + 1", modControl | modAlt, 0x31, "Strg+Alt+1"},
		{"Ctrl+Shift+F12", modControl | modShift, 0x7B, "Strg+Umschalt+F12"},
		{"Win+Alt+D", modWin | modAlt, 0x44, "Alt+Win+D"},
		{"AltGr+B", modControl | modAlt, 0x42, "Strg+Alt+B"},
		{"Rollen", 0, 0x91, "Rollen"},
		{"Bild Auf", 0, 0x21, "BildAuf"},
		{"Menü", 0, 0x5D, "Menue"},
		{"Num7", 0, 0x67, "Num7"},
		{"F24", 0, 0x87, "F24"},
		{"Strg+0x13", modControl, 0x03, "Strg+Untbr"},
		{"Strg+Pause", modControl, 0x03, "Strg+Untbr"},
		{"Alt+Pause", modAlt, 0x13, "Alt+Pause"},
		{"Win+0xE8", modWin, 0xE8, "Win+0xE8"},
		{"Strg+Rück", modControl, 0x08, "Strg+Ruecktaste"},
	}
	for _, c := range cases {
		h, err := parseHotkey(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if h.Mods != c.mods || h.VK != c.vk || h.String() != c.out {
			t.Errorf("%q: mods %#x vk %#x %q, erwartet %#x %#x %q", c.in, h.Mods, h.VK, h.String(), c.mods, c.vk, c.out)
		}
		// Was String ausgibt, muss sich wieder lesen lassen.
		again, err := parseHotkey(h.String())
		if err != nil || again != h {
			t.Errorf("%q: %q lässt sich nicht wieder lesen (%v)", c.in, h.String(), err)
		}
	}
}

func TestParseHotkeyErrors(t *testing.T) {
	for _, in := range []string{"", "Strg", "Strg+", "Strg++1", "Hyper+1", "1+Strg", "Strg+Alt+Blubb", "0x00", "0x1FF"} {
		if h, err := parseHotkey(in); err == nil {
			t.Errorf("%q sollte nicht gehen, ergab %v", in, h)
		}
	}
}

func TestBlocksTyping(t *testing.T) {
	cases := map[string]bool{
		"A": true, "Umschalt+A": true, "Leertaste": true, "Num1": true,
		"Strg+A": false, "Alt+1": false, "Win+Leertaste": false,
		"Pause": false, "F12": false, "Rollen": false, "Menue": false,
	}
	for in, want := range cases {
		h, err := parseHotkey(in)
		if err != nil {
			t.Fatal(in, err)
		}
		if h.blocksTyping() != want {
			t.Errorf("%s: blocksTyping %v, erwartet %v", in, !want, want)
		}
	}
}

func TestKeyNamesAreUnique(t *testing.T) {
	seenName := map[string]string{}
	seenVK := map[uint32]string{}
	for _, k := range keyNames {
		if prev, ok := seenVK[k.vk]; ok {
			t.Errorf("Code %#x doppelt: %s und %s", k.vk, prev, k.display)
		}
		seenVK[k.vk] = k.display
		for _, n := range k.names {
			if n != normalizeKeyText(n) {
				t.Errorf("Name %q ist nicht normalisiert", n)
			}
			if _, isMod := modNames[n]; isMod {
				t.Errorf("Name %q ist auch eine Umschalttaste", n)
			}
			if prev, ok := seenName[n]; ok {
				t.Errorf("Name %q doppelt: %s und %s", n, prev, k.display)
			}
			seenName[n] = k.display
		}
	}
}
