// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Eine Taste aufnehmen: aus den einzelnen Tastendrücken, die der
// Tastatur-Hook meldet, wird ein Kürzel wie Strg+Alt+2. Nur Rechnen, ohne
// Windows, damit die Tests unter Linux laufen. Den Hook selbst setzt
// setup_windows.go.
package main

// vkEscape bricht die Aufnahme ab, solange keine Umschalttaste gedrückt ist.
const vkEscape = 0x1B

// modifierBit sagt, welche Umschalttaste ein Tastencode ist. Der Hook
// meldet links und rechts getrennt (0xA0 bis 0xA5), ältere Wege die
// allgemeinen Codes (0x10 bis 0x12).
func modifierBit(vk uint32) uint32 {
	switch vk {
	case 0x10, 0xA0, 0xA1:
		return modShift
	case 0x11, 0xA2, 0xA3:
		return modControl
	case 0x12, 0xA4, 0xA5:
		return modAlt
	case 0x5B, 0x5C:
		return modWin
	}
	return 0
}

// captureEvent ist, was ein Tastendruck während der Aufnahme bewirkt.
type captureEvent int

const (
	captureNone      captureEvent = iota // weiter warten
	captureDone                          // Kürzel steht fest
	captureCancelled                     // Esc ohne Umschalttaste
)

// keyCapture verfolgt die Aufnahme. Solange sie läuft, wird jeder
// Tastendruck verschluckt: Win allein öffnet sonst das Startmenü, Alt die
// Menüleiste, und die gesuchte Taste soll in keinem Programm ankommen.
//
// Nach dem Ende werden noch die Loslass-Ereignisse der Tasten verschluckt,
// deren Druck verschluckt wurde. Ein Loslassen ohne Drücken verwirrt
// sonst manche Programme (und Win kann das Startmenü öffnen).
type keyCapture struct {
	active bool
	mods   uint32
	held   map[uint32]bool // verschluckte Drücke, deren Loslassen noch kommt
}

func (c *keyCapture) start() {
	c.active = true
	c.mods = 0
	if c.held == nil {
		c.held = map[uint32]bool{}
	}
}

// finished: Aufnahme vorbei und alles losgelassen, der Hook kann weg.
func (c *keyCapture) finished() bool { return !c.active && len(c.held) == 0 }

// feed bekommt jeden Tastendruck (down) und jedes Loslassen. swallow sagt
// dem Hook, ob Windows das Ereignis weitergeben soll.
func (c *keyCapture) feed(vk uint32, down bool) (swallow bool, ev captureEvent, h hotkey) {
	if !c.active {
		if !down && c.held[vk] {
			delete(c.held, vk)
			return true, captureNone, hotkey{}
		}
		return false, captureNone, hotkey{}
	}
	bit := modifierBit(vk)
	if !down {
		if bit != 0 {
			c.mods &^= bit
		}
		if c.held[vk] {
			delete(c.held, vk)
			return true, captureNone, hotkey{}
		}
		return false, captureNone, hotkey{} // war schon vor der Aufnahme gedrückt
	}
	c.held[vk] = true
	if bit != 0 {
		c.mods |= bit
		return true, captureNone, hotkey{}
	}
	c.active = false
	if vk == vkEscape && c.mods == 0 {
		return true, captureCancelled, hotkey{}
	}
	h = hotkey{Mods: c.mods, VK: vk}
	if h.VK == vkPause && h.Mods&modControl != 0 {
		h.VK = vkCancel // so meldet Windows Strg+Pause ohnehin, sicher ist sicher
	}
	return true, captureDone, h
}

// stop bricht ab (Fenster zu, Zeit abgelaufen). Was noch gedrückt ist,
// wird beim Loslassen weiter verschluckt.
func (c *keyCapture) stop() { c.active = false }
