// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Tastenkürzel wie "Strg+Alt+1" oder "Pause" lesen und einheitlich wieder
// ausgeben. Nur Rechnen, kein Windows: läuft in den Tests unter Linux.
package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Umschalttasten, wie RegisterHotKey sie erwartet (MOD_*).
const (
	modAlt     = 0x0001
	modControl = 0x0002
	modShift   = 0x0004
	modWin     = 0x0008
	// modNoRepeat: gedrückt halten löst nur einmal aus, nicht im Takt der
	// Tastenwiederholung.
	modNoRepeat = 0x4000
)

// hotkey ist eine Taste mit ihren Umschalttasten.
type hotkey struct {
	Mods uint32 // modAlt | modControl | ...
	VK   uint32 // virtueller Tastencode von Windows
}

// keyName ist eine Taste, die man in die INI schreiben darf.
type keyName struct {
	vk      uint32
	display string   // so steht sie in Meldungen und im Menü
	names   []string // so darf man sie schreiben (klein, ohne Umlaute und Leerzeichen)
	// common: ohne Strg, Alt oder Win wäre die Taste beim normalen Tippen
	// weg (Buchstaben, Ziffern, Leertaste ...). Erlaubt, aber mit Warnung.
	common bool
}

// keyNames sind alle Tasten mit Namen. Buchstaben, Ziffern und F1 bis F24
// kommen in init dazu.
var keyNames = []keyName{
	{0x13, "Pause", []string{"pause", "untbr"}, false},
	{0x91, "Rollen", []string{"rollen", "scrolllock", "scroll", "rollenfeststelltaste"}, false},
	{0x2C, "Druck", []string{"druck", "print", "printscreen", "prtsc", "drucken"}, false},
	{0x2D, "Einfg", []string{"einfg", "einfuegen", "insert", "ins"}, false},
	{0x2E, "Entf", []string{"entf", "entfernen", "delete", "del"}, true},
	{0x24, "Pos1", []string{"pos1", "home"}, true},
	{0x23, "Ende", []string{"ende", "end"}, true},
	{0x21, "BildAuf", []string{"bildauf", "pageup", "pgup"}, true},
	{0x22, "BildAb", []string{"bildab", "pagedown", "pgdn"}, true},
	{0x5D, "Menue", []string{"menue", "menu", "kontextmenue", "anwendung", "apps"}, false},
	{0x20, "Leertaste", []string{"leertaste", "leer", "space"}, true},
	{0x1B, "Esc", []string{"esc", "escape"}, true},
	{0x09, "Tab", []string{"tab"}, true},
	{0x0D, "Enter", []string{"enter", "eingabe", "return"}, true},
	{0x08, "Ruecktaste", []string{"ruecktaste", "rueck", "backspace"}, true},
	{0x25, "Links", []string{"links", "left", "pfeillinks"}, true},
	{0x26, "Hoch", []string{"hoch", "oben", "up", "pfeilhoch"}, true},
	{0x27, "Rechts", []string{"rechts", "right", "pfeilrechts"}, true},
	{0x28, "Runter", []string{"runter", "unten", "down", "pfeilrunter"}, true},
	{0xBB, "Plus", []string{"plus"}, true},
	{0xBD, "Minus", []string{"minus"}, true},
	{0xBC, "Komma", []string{"komma", "comma"}, true},
	{0xBE, "Punkt", []string{"punkt", "period"}, true},
	{0x6A, "NumMal", []string{"nummal", "multiply", "nummultiply"}, true},
	{0x6B, "NumPlus", []string{"numplus", "add", "numadd"}, true},
	{0x6D, "NumMinus", []string{"numminus", "subtract", "numsubtract"}, true},
	{0x6E, "NumKomma", []string{"numkomma", "decimal", "numdecimal"}, true},
	{0x6F, "NumGeteilt", []string{"numgeteilt", "divide", "numdivide"}, true},
	{0xAD, "Stumm", []string{"stumm", "volumemute", "mute"}, false},
	{0xAE, "Leiser", []string{"leiser", "volumedown"}, false},
	{0xAF, "Lauter", []string{"lauter", "volumeup"}, false},
	{0xB3, "PlayPause", []string{"playpause", "abspielen", "wiedergabe"}, false},
	{0xB2, "Stopp", []string{"stopp", "stop", "mediastop"}, false},
	{0xB0, "NaechsterTitel", []string{"naechstertitel", "medianext", "nexttrack"}, false},
	{0xB1, "VorigerTitel", []string{"vorigertitel", "mediaprev", "prevtrack"}, false},
}

func init() {
	for c := 'A'; c <= 'Z'; c++ {
		keyNames = append(keyNames, keyName{uint32(c), string(c), []string{strings.ToLower(string(c))}, true})
	}
	for c := '0'; c <= '9'; c++ {
		keyNames = append(keyNames, keyName{uint32(c), string(c), []string{string(c)}, true})
		n := string(c)
		keyNames = append(keyNames, keyName{0x60 + uint32(c-'0'), "Num" + n, []string{"num" + n, "numpad" + n, "ziffernblock" + n}, true})
	}
	// F1 bis F24: 0x70 bis 0x87. F13 bis F24 haben viele Tastaturen nicht,
	// Makrotasten und Programme wie AutoHotkey können sie aber senden.
	for i := 1; i <= 24; i++ {
		n := "F" + strconv.Itoa(i)
		keyNames = append(keyNames, keyName{0x6F + uint32(i), n, []string{strings.ToLower(n)}, false})
	}
}

// modNames: so darf man die Umschalttasten schreiben. AltGr ist für
// Windows dasselbe wie Strg+Alt.
var modNames = map[string]uint32{
	"strg": modControl, "ctrl": modControl, "control": modControl, "steuerung": modControl,
	"alt":      modAlt,
	"umschalt": modShift, "shift": modShift, "hochstelltaste": modShift,
	"win": modWin, "windows": modWin,
	"altgr": modControl | modAlt,
}

// normalizeKeyText macht "Bild Auf", "BILDAUF" und "bildauf" gleich und
// erlaubt Umlaute ("Menü", "Rück").
func normalizeKeyText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", " ", "", "_", "", "-", "").Replace(s)
	return s
}

// parseHotkey liest ein Kürzel wie "Strg+Alt+1", "Pause" oder "Win+0x13".
func parseHotkey(text string) (hotkey, error) {
	parts := strings.Split(text, "+")
	var h hotkey
	for i, raw := range parts {
		p := normalizeKeyText(raw)
		if p == "" {
			return hotkey{}, errors.New("leerer Teil, steht da ein + zu viel?")
		}
		last := i == len(parts)-1
		if mod, ok := modNames[p]; ok {
			if last {
				return hotkey{}, errors.New("es fehlt die eigentliche Taste nach " + strings.TrimSpace(raw))
			}
			h.Mods |= mod
			continue
		}
		if !last {
			return hotkey{}, fmt.Errorf("%q ist keine Umschalttaste (Strg, Alt, Umschalt, Win, AltGr)", strings.TrimSpace(raw))
		}
		vk, ok := lookupKey(p)
		if !ok {
			return hotkey{}, fmt.Errorf("die Taste %q kenne ich nicht", strings.TrimSpace(raw))
		}
		h.VK = vk
	}
	return h, nil
}

func lookupKey(p string) (uint32, bool) {
	for _, k := range keyNames {
		for _, n := range k.names {
			if n == p {
				return k.vk, true
			}
		}
	}
	// Tasten ohne Namen als Code von Windows, z. B. 0x13 für Pause.
	if strings.HasPrefix(p, "0x") {
		if n, err := strconv.ParseUint(p[2:], 16, 8); err == nil && n > 0 && n < 0xFF {
			return uint32(n), true
		}
	}
	return 0, false
}

// String ist das Kürzel in einer festen Schreibweise: Strg, Alt, Umschalt,
// Win, dann die Taste.
func (h hotkey) String() string {
	var b strings.Builder
	for _, m := range []struct {
		bit  uint32
		name string
	}{{modControl, "Strg"}, {modAlt, "Alt"}, {modShift, "Umschalt"}, {modWin, "Win"}} {
		if h.Mods&m.bit != 0 {
			b.WriteString(m.name + "+")
		}
	}
	if k, ok := keyByVK(h.VK); ok {
		b.WriteString(k.display)
	} else {
		fmt.Fprintf(&b, "0x%02X", h.VK)
	}
	return b.String()
}

func keyByVK(vk uint32) (keyName, bool) {
	for _, k := range keyNames {
		if k.vk == vk {
			return k, true
		}
	}
	return keyName{}, false
}

// blocksTyping: ohne Strg, Alt oder Win nimmt das Kürzel eine Taste weg,
// die man beim Schreiben braucht. Umschalt allein hilft nicht ("Umschalt+A"
// ist ein großes A).
func (h hotkey) blocksTyping() bool {
	if h.Mods&(modControl|modAlt|modWin) != 0 {
		return false
	}
	k, ok := keyByVK(h.VK)
	return ok && k.common
}
