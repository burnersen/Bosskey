// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Tests für das Aufnehmen einer Taste aus einzelnen Tastendrücken.
package main

import "testing"

type keyEvent struct {
	vk   uint32
	down bool
}

func press(vks ...uint32) []keyEvent {
	var ev []keyEvent
	for _, vk := range vks {
		ev = append(ev, keyEvent{vk, true})
	}
	for i := len(vks) - 1; i >= 0; i-- {
		ev = append(ev, keyEvent{vks[i], false})
	}
	return ev
}

func TestCaptureCombinations(t *testing.T) {
	const lctrl, ralt, lshift, lwin, lalt = 0xA2, 0xA5, 0xA0, 0x5B, 0xA4
	cases := []struct {
		name string
		keys []keyEvent
		want string
	}{
		{"Pause allein", press(vkPause), "Pause"},
		{"Strg+Alt+2", press(lctrl, lalt, '2'), "Strg+Alt+2"},
		{"Win+Umschalt+F5", press(lwin, lshift, 0x74), "Umschalt+Win+F5"},
		{"AltGr+B", press(lctrl, ralt, 'B'), "Strg+Alt+B"},
		{"Strg+Pause wird Untbr", press(lctrl, vkPause), "Strg+Untbr"},
		{"Strg+Esc ist eine Taste", press(lctrl, vkEscape), "Strg+Esc"},
	}
	for _, c := range cases {
		var kc keyCapture
		kc.start()
		var got hotkey
		done := false
		for _, e := range c.keys {
			swallow, ev, h := kc.feed(e.vk, e.down)
			if !swallow {
				t.Errorf("%s: %#x down=%v nicht verschluckt", c.name, e.vk, e.down)
			}
			if ev == captureDone {
				got, done = h, true
			}
		}
		if !done || got.String() != c.want {
			t.Errorf("%s: %q (fertig %v), erwartet %q", c.name, got, done, c.want)
		}
		if !kc.finished() {
			t.Errorf("%s: nach dem Loslassen nicht fertig, Hook bliebe hängen", c.name)
		}
	}
}

func TestCaptureEscapeCancels(t *testing.T) {
	var kc keyCapture
	kc.start()
	swallow, ev, _ := kc.feed(vkEscape, true)
	if !swallow || ev != captureCancelled {
		t.Fatalf("Esc: %v %v", swallow, ev)
	}
	if kc.finished() {
		t.Fatal("Loslassen von Esc steht noch aus")
	}
	if swallow, _, _ := kc.feed(vkEscape, false); !swallow || !kc.finished() {
		t.Fatal("Loslassen von Esc nicht verschluckt")
	}
}

func TestCaptureLeavesOtherKeysAlone(t *testing.T) {
	var kc keyCapture
	// Vor der Aufnahme: nichts verschlucken.
	if swallow, _, _ := kc.feed('A', true); swallow {
		t.Fatal("ohne Aufnahme verschluckt")
	}
	kc.start()
	// Taste war schon vor der Aufnahme unten: ihr Loslassen geht durch.
	if swallow, _, _ := kc.feed('A', false); swallow {
		t.Fatal("fremdes Loslassen verschluckt")
	}
	kc.feed(0xA2, true)
	kc.feed('X', true) // fertig: Strg+X
	// Nach der Aufnahme: neue Tasten gehen durch, nur das ausstehende Loslassen nicht.
	if swallow, _, _ := kc.feed('Y', true); swallow {
		t.Fatal("neue Taste nach der Aufnahme verschluckt")
	}
	if swallow, _, _ := kc.feed('X', false); !swallow {
		t.Fatal("Loslassen von X durchgelassen")
	}
	if swallow, _, _ := kc.feed(0xA2, false); !swallow || !kc.finished() {
		t.Fatal("Loslassen von Strg durchgelassen")
	}
}

func TestCaptureStop(t *testing.T) {
	var kc keyCapture
	kc.start()
	kc.feed(0x5B, true) // Win gedrückt, dann Fenster zu
	kc.stop()
	if kc.finished() {
		t.Fatal("Win ist noch unten, Hook muss bleiben")
	}
	if swallow, _, _ := kc.feed(0x5B, false); !swallow || !kc.finished() {
		t.Fatal("Loslassen von Win nach stop nicht verschluckt")
	}
}
