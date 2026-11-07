// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Den aktuellen Desktop aus der Registry lesen und mit Strg+Win+Pfeil
// wechseln.
//
// Warum so: Windows hat keine offizielle Schnittstelle, um auf Desktop X zu
// springen. Die inoffizielle (IVirtualDesktopManagerInternal) ändert sich
// mit fast jedem größeren Windows-Update und bringt Programme dann zum
// Absturz. Die Registry-Werte und Strg+Win+Pfeil gibt es seit Windows 10
// unverändert. Nachteil: man sieht die Schiebe-Animation über die Desktops
// dazwischen.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	// vdKey hat die Liste aller Desktops (VirtualDesktopIDs), unter
	// Windows 11 auch den aktuellen (CurrentVirtualDesktop).
	vdKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\VirtualDesktops`
	// sessionVDKey hat unter Windows 10 den aktuellen Desktop, je Sitzung.
	sessionVDKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\SessionInfo\%d\VirtualDesktops`

	// win11Build: ab diesem Build ist es Windows 11 und der aktuelle Desktop
	// steht direkt unter vdKey.
	win11Build = 22000
)

// Zeiten für den Wechsel. Explorer schreibt den neuen Desktop beim Start
// der Animation in die Registry, also nach wenigen Millisekunden.
const (
	modifierWait = 1500 * time.Millisecond // so lange warten, bis der Nutzer Strg/Alt/Win loslässt
	changeWait   = 800 * time.Millisecond  // so lange auf den neuen Desktop warten, sonst gilt der Schritt als gescheitert
	pollEvery    = 15 * time.Millisecond
	blindPause   = 120 * time.Millisecond // zwischen Schritten, wenn der aktuelle Desktop unbekannt ist
)

var errNotFound = errors.New("Registry-Wert fehlt")

// desktopState ist, was die Registry gerade sagt.
type desktopState struct {
	Pos   int  // ab 0
	Count int  // mindestens 1
	Known bool // false: Anzahl bekannt, Position nicht (CurrentVirtualDesktop fehlt)
}

func readDesktops() (desktopState, error) {
	ids, err := regBinary(vdKey, "VirtualDesktopIDs")
	if err != nil && !errors.Is(err, errNotFound) {
		return desktopState{}, err
	}
	current, err := currentDesktopID()
	if err != nil && !errors.Is(err, errNotFound) {
		return desktopState{}, err
	}
	pos, count, err := desktopPosition(ids, current)
	if errors.Is(err, errCurrentUnknown) {
		return desktopState{Count: count}, nil
	}
	if err != nil {
		return desktopState{}, err
	}
	return desktopState{Pos: pos, Count: count, Known: true}, nil
}

// currentDesktopID liest den aktuellen Desktop dort, wo diese Windows-
// Version ihn hinschreibt, und zur Sicherheit danach am anderen Ort.
func currentDesktopID() ([]byte, error) {
	session := fmt.Sprintf(sessionVDKey, sessionID())
	places := []string{session, vdKey}
	if windowsBuild() >= win11Build {
		places = []string{vdKey, session}
	}
	var lastErr error = errNotFound
	for _, key := range places {
		v, err := regBinary(key, "CurrentVirtualDesktop")
		if err == nil && len(v) == guidSize {
			return v, nil
		}
		if err != nil && !errors.Is(err, errNotFound) {
			lastErr = err
		}
	}
	return nil, lastErr
}

// switchDesktop wechselt auf den Desktop dest (ab 0). Schritt für Schritt:
// eine Taste, warten, bis die Registry den neuen Desktop zeigt, nächste.
// So schießt es nie übers Ziel hinaus, auch wenn ein Schritt verloren geht.
func switchDesktop(dest int, logger *slog.Logger) error {
	waitForModifiersReleased(logger)

	st, err := readDesktops()
	if err != nil {
		return userError(err, "Kann die virtuellen Desktops nicht lesen", "Einzelheiten stehen im Log.")
	}
	if dest >= st.Count {
		return userError(nil, fmt.Sprintf("Desktop %d gibt es nicht, du hast gerade %d", dest+1, st.Count),
			"Leg einen neuen mit Win+Strg+D an oder ändere die Nummer in den Einstellungen.")
	}
	if !st.Known {
		return switchBlind(dest, st.Count, logger)
	}

	triedTaskbar := false
	// Höchstens so viele Schritte, wie es Desktops gibt, plus Luft für
	// einen Wiederholversuch: eine Schleife ohne Ende darf es nicht geben.
	for steps := 0; steps <= st.Count+1; {
		if st.Pos == dest {
			logger.Info("switched", "desktop", dest+1, "steps", steps)
			return nil
		}
		dir := uint16(vkRight)
		if dest < st.Pos {
			dir = vkLeft
		}
		sendDesktopKey(dir)
		next, changed := waitForDesktopChange(st.Pos)
		if changed {
			st = next
			steps++
			continue
		}
		// Nichts passiert. Häufigster Grund: ein Programm mit Adminrechten
		// liegt vorne und Windows lässt die Tasten nicht durch. Einmal die
		// Taskleiste nach vorne holen und nochmal.
		if !triedTaskbar {
			triedTaskbar = true
			logger.Warn("switch step had no effect, focusing taskbar", "from", st.Pos+1, "to", dest+1)
			focusTaskbar()
			continue
		}
		return userError(nil, "Der Desktop-Wechsel hat nicht geklappt",
			"Oft liegt ein Programm mit Adminrechten im Vordergrund. Klick einmal auf die Taskleiste und drück die Taste nochmal.")
	}
	return userError(nil, "Der Desktop-Wechsel hat nicht geklappt", "Einzelheiten stehen im Log.")
}

// switchBlind wechselt, wenn Windows den aktuellen Desktop nicht verrät
// (kommt nach einem Neustart vor, bis zum ersten Wechsel von Hand): erst
// ganz nach links, das endet sicher am Rand bei Desktop 1, dann nach rechts
// abzählen.
func switchBlind(dest, count int, logger *slog.Logger) error {
	logger.Info("current desktop unknown, switching blind", "to", dest+1, "count", count)
	for range count - 1 {
		sendDesktopKey(vkLeft)
		time.Sleep(blindPause)
	}
	for range dest {
		sendDesktopKey(vkRight)
		time.Sleep(blindPause)
	}
	return nil
}

func waitForDesktopChange(from int) (desktopState, bool) {
	deadline := time.Now().Add(changeWait)
	for time.Now().Before(deadline) {
		time.Sleep(pollEvery)
		st, err := readDesktops()
		if err == nil && st.Known && st.Pos != from {
			return st, true
		}
	}
	return desktopState{}, false
}

// waitForModifiersReleased wartet, bis Strg, Alt, Umschalt und Win
// losgelassen sind. Hält der Nutzer noch Alt von "Strg+Alt+1" fest, würde
// aus unserem Strg+Win+Pfeil ein Strg+Alt+Win+Pfeil, und das wechselt nicht.
func waitForModifiersReleased(logger *slog.Logger) {
	deadline := time.Now().Add(modifierWait)
	for time.Now().Before(deadline) {
		if !anyModifierDown() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	logger.Warn("modifier keys still held, switching anyway")
}

func anyModifierDown() bool {
	for _, vk := range []uintptr{vkShift, vkControl, vkMenu, vkLWin, vkRWin} {
		r, _, _ := procGetAsyncKeyState.Call(vk)
		if r&0x8000 != 0 {
			return true
		}
	}
	return false
}

// sendDesktopKey drückt Strg+Win+Pfeil in einem Rutsch (ein SendInput mit
// sechs Ereignissen), damit nichts anderes dazwischenkommt. Win und die
// Pfeile sind "erweiterte" Tasten und brauchen das Kennzeichen dafür.
func sendDesktopKey(arrow uint16) {
	key := func(vk uint16, flags uint32) keyboardInput {
		scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0) // MAPVK_VK_TO_VSC
		return keyboardInput{inputType: inputKeyboard, wVk: vk, wScan: uint16(scan), dwFlags: flags}
	}
	ext := uint32(keyEventExtendedKey)
	in := []keyboardInput{
		key(vkLControl, 0),
		key(vkLWin, ext),
		key(arrow, ext),
		key(arrow, ext|keyEventKeyUp),
		key(vkLWin, ext|keyEventKeyUp),
		key(vkLControl, keyEventKeyUp),
	}
	procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

// focusTaskbar holt die Taskleiste nach vorne. Bosskey darf das, weil es
// gerade die letzte Eingabe (den Hotkey) bekommen hat.
func focusTaskbar() {
	cls := utf16Ptr("Shell_TrayWnd")
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	if h != 0 {
		procSetForegroundWindow.Call(h)
		time.Sleep(50 * time.Millisecond)
	}
}

// regBinary liest einen REG_BINARY-Wert unter HKEY_CURRENT_USER.
func regBinary(path, name string) ([]byte, error) {
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, utf16Ptr(path), 0, syscall.KEY_READ, &key); err != nil {
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) || errors.Is(err, syscall.ERROR_PATH_NOT_FOUND) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("Registry-Schlüssel öffnen: %w", err)
	}
	defer syscall.RegCloseKey(key) // nur gelesen: ein Fehler beim Schließen ändert nichts

	n := utf16Ptr(name)
	var typ, size uint32
	if err := syscall.RegQueryValueEx(key, n, nil, &typ, nil, &size); err != nil {
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("Registry-Wert %s lesen: %w", name, err)
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	if err := syscall.RegQueryValueEx(key, n, nil, &typ, &buf[0], &size); err != nil {
		return nil, fmt.Errorf("Registry-Wert %s lesen: %w", name, err)
	}
	return buf[:size], nil
}

func sessionID() uint32 {
	var id uint32
	procProcessIdToSessionId.Call(uintptr(syscall.Getpid()), uintptr(unsafe.Pointer(&id)))
	return id
}

// windowsBuild ist die Build-Nummer (19045 für Windows 10 22H2, ab 22000
// Windows 11). RtlGetVersion sagt die Wahrheit, GetVersionEx lügt ohne
// passendes Manifest.
func windowsBuild() uint32 {
	var v osVersionInfo
	v.size = uint32(unsafe.Sizeof(v))
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&v)))
	return v.build
}

// describeDesktops ist eine Zeile fürs Log.
func describeDesktops(st desktopState) string {
	if !st.Known {
		return strconv.Itoa(st.Count) + " Desktops, aktueller unbekannt"
	}
	return fmt.Sprintf("Desktop %d von %d", st.Pos+1, st.Count)
}
