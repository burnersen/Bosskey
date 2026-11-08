// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Das Fenster "Tasten einrichten": Liste der Tasten, Desktop auswählen,
// "Taste aufnehmen" klicken, Taste drücken. Ohne Text in einer Datei.
//
// Aufgenommen wird mit einem Tastatur-Hook (WH_KEYBOARD_LL). Nur der sieht
// auch Kombinationen mit Win und kann sie verschlucken, bevor Windows das
// Startmenü öffnet oder ein Programm sie bekommt. Der Hook hängt nur
// während der Aufnahme und fällt spätestens nach captureTimeout von selbst.
package main

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	setupClassName = "BosskeySetupWindow"

	idList   = 201
	idRemove = 202
	idCombo  = 203
	idRecord = 204
	idClose  = 205
	idOK     = 1 // Enter, von IsDialogMessage
	idCancel = 2 // Esc, von IsDialogMessage

	wmCaptured         = wmApp + 20 // wParam Umschalttasten, lParam Tastencode
	wmCaptureCancelled = wmApp + 21
	wmUnhook           = wmApp + 22 // an das Tray-Fenster: Hook kann weg

	timerCaptureTimeout = 3
	// captureTimeout: so lange wartet die Aufnahme auf eine Taste. Danach
	// gibt der Hook die Tastatur sicher wieder frei.
	captureTimeout = 15000 // Millisekunden

	// testHotkeyID: für die Probe, ob eine Taste frei ist. Liegt weit weg
	// von den IDs der echten Tasten (ab 1).
	testHotkeyID = 0xBF00

	whKeyboardLL     = 13
	wmKeyDown        = 0x0100
	wmSysKeyDown     = 0x0104
	wmCommand        = 0x0111
	wmSetFont        = 0x0030
	llkhfInjected    = 0x10
	wsOverlapped     = 0x00C00000 | 0x00080000 | 0x00020000 // Titel, Systemmenü, Minimieren
	wsChild          = 0x40000000
	wsVisible        = 0x10000000
	wsTabStop        = 0x00010000
	wsVScroll        = 0x00200000
	wsExClientEdge   = 0x00000200
	wsExControlParen = 0x00010000
	wsExAppWindow    = 0x00040000
	bsDefPushButton  = 0x1
	lbsNotify        = 0x1
	lbsNoIntegral    = 0x100
	cbsDropDownList  = 0x3
	ssNoPrefix       = 0x80
	lbAddString      = 0x0180
	lbResetContent   = 0x0184
	lbSetCurSel      = 0x0186
	lbGetCurSel      = 0x0188
	lbnDblClk        = 2
	cbAddString      = 0x0143
	cbResetContent   = 0x014B
	cbGetCurSel      = 0x0147
	cbSetCurSel      = 0x014E
	colorBtnFace     = 15
	idcArrow         = 32512
	swShow           = 5
	swRestore        = 9
	mbYesNo          = 0x4
	mbIconQuestion   = 0x20
	idYes            = 6
)

// keyHook ist der Tastatur-Hook mit seiner Aufnahme. Er gehört nicht zum
// Fenster: schließt der Nutzer das Fenster mitten in der Aufnahme, müssen
// die Loslass-Ereignisse trotzdem noch verschluckt werden (capture.go).
var keyHook struct {
	handle  uintptr
	c       keyCapture
	notify  uintptr // Setup-Fenster, das das Ergebnis bekommt; 0 wenn zu
	tray    uintptr // Tray-Fenster, räumt den Hook ab
	endedAt time.Time
}

var (
	hookCallback  = sync.OnceValue(func() uintptr { return syscall.NewCallback(lowLevelKeyboardProc) })
	setupCallback = sync.OnceValue(func() uintptr { return syscall.NewCallback(setupWndProc) })
	setupClass    sync.Once
	setupClassErr error
	uiFonts       = sync.OnceValue(createFonts)
)

type fonts struct{ normal, big uintptr }

// setupWindow ist das offene Fenster. Es gibt höchstens eins.
type setupWindow struct {
	t                                   *tray
	hwnd                                uintptr
	list, remove, combo, record, prompt uintptr
	targets                             []target // was im Auswahlfeld steht, in derselben Reihenfolge
	replace                             int      // Eintrag, dessen Taste neu aufgenommen wird, sonst -1
	captureTarget                       target
	capturing                           bool
	dpi                                 int
}

var theSetup *setupWindow

// openSetup öffnet das Fenster oder holt es nach vorne.
func (t *tray) openSetup(welcome bool) {
	if theSetup != nil {
		procShowWindow.Call(theSetup.hwnd, swRestore)
		procSetForegroundWindow.Call(theSetup.hwnd)
		return
	}
	s := &setupWindow{t: t, replace: -1, dpi: systemDPI()}
	if err := s.create(); err != nil {
		t.logger.Error("open setup window", "err", err)
		t.notify("Kann das Fenster nicht öffnen", "Einzelheiten stehen im Protokoll.", true)
		return
	}
	theSetup = s
	s.fill()
	if welcome {
		s.setPrompt("Willkommen! Die Taste Pause bringt dich schon auf Desktop 1.\r\n" +
			"Für mehr: Desktop auswählen, auf „Taste aufnehmen“ klicken, Taste drücken.")
	}
	procShowWindow.Call(s.hwnd, swShow)
	procSetForegroundWindow.Call(s.hwnd)
	procSetFocus.Call(s.record)
	t.logger.Info("setup window opened")
}

func systemDPI() int {
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d >= 96 {
			return int(d)
		}
	}
	return 96
}

// px rechnet Maße für 96 dpi auf den Bildschirm um.
func (s *setupWindow) px(v int) uintptr { return uintptr(v * s.dpi / 96) }

func createFonts() fonts {
	dpi := systemDPI()
	mk := func(pt, weight int) uintptr {
		face := utf16Ptr("Segoe UI")
		h, _, _ := procCreateFontW.Call(uintptr(-pt*dpi/72)&0xFFFFFFFF, 0, 0, 0, uintptr(weight), 0, 0, 0,
			1 /* DEFAULT_CHARSET */, 0, 0, 5 /* CLEARTYPE_QUALITY */, 0, uintptr(unsafe.Pointer(face)))
		return h
	}
	return fonts{normal: mk(9, 400), big: mk(11, 600)}
}

func (s *setupWindow) create() error {
	hinst, _, _ := procGetModuleHandleW.Call(0)
	setupClass.Do(func() {
		cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		wc := wndClassEx{
			lpfnWndProc:   setupCallback(),
			hInstance:     hinst,
			hIcon:         s.t.icon,
			hIconSm:       s.t.icon,
			hCursor:       cursor,
			hbrBackground: colorBtnFace + 1,
			lpszClassName: utf16Ptr(setupClassName),
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			setupClassErr = fmt.Errorf("RegisterClassEx: %w", err)
		}
	})
	if setupClassErr != nil {
		return setupClassErr
	}

	// Innenmaß 460 x 400 bei 96 dpi, außen kommen Rahmen und Titel dazu.
	r := rect{right: int32(s.px(460)), bottom: int32(s.px(400))}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), wsOverlapped, 0, wsExControlParen|wsExAppWindow)
	w, h := r.right-r.left, r.bottom-r.top
	sw, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	sh, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	x, y := (int32(sw)-w)/2, (int32(sh)-h)/2

	class, title := utf16Ptr(setupClassName), utf16Ptr(appName+": Tasten einrichten")
	hwnd, _, err := procCreateWindowExW.Call(wsExControlParen|wsExAppWindow, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		wsOverlapped, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, hinst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	s.hwnd = hwnd
	theSetup = s // die Fensterfunktion braucht es schon während der Steuerelemente

	f := uiFonts()
	s.child("STATIC", "Welche Taste bringt dich auf welchen Desktop?", ssNoPrefix, 12, 12, 436, 24, 0, f.big)
	s.list = s.child("LISTBOX", "", wsTabStop|wsVScroll|lbsNotify|lbsNoIntegral, 12, 42, 436, 150, idList, f.normal)
	s.remove = s.child("BUTTON", "Entfernen", wsTabStop, 12, 200, 110, 28, idRemove, f.normal)
	s.child("STATIC", "Doppelklick auf einen Eintrag: Taste neu aufnehmen", ssNoPrefix, 134, 206, 314, 20, 0, f.normal)
	s.child("STATIC", "Neue Taste für:", ssNoPrefix, 12, 252, 100, 20, 0, f.normal)
	s.combo = s.child("COMBOBOX", "", wsTabStop|wsVScroll|cbsDropDownList, 112, 248, 196, 320, idCombo, f.normal)
	s.record = s.child("BUTTON", "Taste aufnehmen", wsTabStop|bsDefPushButton, 318, 246, 130, 30, idRecord, f.normal)
	s.prompt = s.child("STATIC", "", ssNoPrefix, 12, 292, 436, 62, 0, f.big)
	s.child("BUTTON", "Schließen", wsTabStop, 338, 362, 110, 28, idClose, f.normal)
	s.setPrompt("")
	return nil
}

// child legt ein Steuerelement an. Maße bei 96 dpi.
func (s *setupWindow) child(class, text string, style uintptr, x, y, w, h int, id uintptr, font uintptr) uintptr {
	var ex uintptr
	if class == "LISTBOX" || class == "COMBOBOX" {
		ex = wsExClientEdge
	}
	c, t := utf16Ptr(class), utf16Ptr(text)
	hwnd, _, _ := procCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)),
		wsChild|wsVisible|style, s.px(x), s.px(y), s.px(w), s.px(h), s.hwnd, id, 0, 0)
	if hwnd != 0 && font != 0 {
		procSendMessageW.Call(hwnd, wmSetFont, font, 1)
	}
	return hwnd
}

// fill schreibt Liste und Auswahlfeld neu.
func (s *setupWindow) fill() {
	procSendMessageW.Call(s.list, lbResetContent, 0, 0)
	for _, b := range s.t.bindings {
		s.addString(s.list, lbAddString, b.Hotkey.String()+"   →   "+b.Target.String())
	}

	count := 1
	if st, err := readDesktops(); err == nil {
		count = st.Count
	}
	n := max(9, count)
	for _, b := range s.t.bindings {
		n = max(n, b.Target.Desktop)
	}
	procSendMessageW.Call(s.combo, cbResetContent, 0, 0)
	s.targets = s.targets[:0]
	for d := 1; d <= n; d++ {
		label := "Desktop " + strconv.Itoa(d)
		if d > count {
			label += " (noch nicht angelegt)"
		}
		s.addString(s.combo, cbAddString, label)
		s.targets = append(s.targets, target{Desktop: d})
	}
	s.addString(s.combo, cbAddString, "Zurück zum vorigen Desktop")
	s.targets = append(s.targets, target{Back: true})
	s.selectTarget(target{Desktop: nextFreeDesktop(s.t.bindings)})
}

func (s *setupWindow) addString(h, msg uintptr, text string) {
	p := utf16Ptr(text)
	procSendMessageW.Call(h, msg, 0, uintptr(unsafe.Pointer(p)))
}

func (s *setupWindow) selectTarget(t target) {
	for i, x := range s.targets {
		if x == t {
			procSendMessageW.Call(s.combo, cbSetCurSel, uintptr(i), 0)
			return
		}
	}
	procSendMessageW.Call(s.combo, cbSetCurSel, 0, 0)
}

func (s *setupWindow) selectedTarget() target {
	i, _, _ := procSendMessageW.Call(s.combo, cbGetCurSel, 0, 0)
	if int(int32(i)) < 0 || int(i) >= len(s.targets) {
		return target{Desktop: 1}
	}
	return s.targets[i]
}

func (s *setupWindow) selectedIndex() int {
	i, _, _ := procSendMessageW.Call(s.list, lbGetCurSel, 0, 0)
	n := int(int32(i)) // LB_ERR ist -1
	if n < 0 || n >= len(s.t.bindings) {
		return -1
	}
	return n
}

func (s *setupWindow) setPrompt(text string) {
	if text == "" {
		text = "Desktop auswählen, auf „Taste aufnehmen“ klicken und die Taste oder Kombination drücken."
	}
	setText(s.prompt, text)
}

func setText(h uintptr, text string) {
	p := utf16Ptr(text)
	procSetWindowTextW.Call(h, uintptr(unsafe.Pointer(p)))
}

func enable(h uintptr, on bool) {
	procEnableWindow.Call(h, boolArg(on))
}

// ---- Aufnahme -------------------------------------------------------------

func (s *setupWindow) startCapture(replace int, t target) {
	if keyHook.handle == 0 {
		hinst, _, _ := procGetModuleHandleW.Call(0)
		h, _, err := procSetWindowsHookExW.Call(whKeyboardLL, hookCallback(), hinst, 0)
		if h == 0 {
			s.t.logger.Error("keyboard hook", "err", err)
			s.setPrompt("Windows lässt die Aufnahme nicht zu. Einzelheiten stehen im Protokoll.")
			return
		}
		keyHook.handle = h
	}
	keyHook.notify = s.hwnd
	keyHook.tray = s.t.hwnd
	keyHook.c.start()
	s.capturing = true
	s.replace = replace
	s.captureTarget = t
	s.selectTarget(t)
	procSetTimer.Call(s.hwnd, timerCaptureTimeout, captureTimeout, 0)
	setText(s.record, "Abbrechen")
	enable(s.list, false)
	enable(s.remove, false)
	enable(s.combo, false)
	s.setPrompt("Jetzt die Taste oder Kombination für " + t.String() + " drücken ...\r\n(Esc bricht ab)")
	s.t.logger.Info("capture started", "target", t.String())
}

// endCapture räumt die Aufnahme ab. Der Hook bleibt, bis alle verschluckten
// Tasten losgelassen sind (meldet sich über wmUnhook beim Tray-Fenster).
func (s *setupWindow) endCapture() {
	procKillTimer.Call(s.hwnd, timerCaptureTimeout)
	keyHook.c.stop()
	keyHook.endedAt = time.Now()
	s.capturing = false
	setText(s.record, "Taste aufnehmen")
	enable(s.list, true)
	enable(s.remove, true)
	enable(s.combo, true)
	maybeUnhook(false)
}

// maybeUnhook nimmt den Hook weg, sobald nichts mehr verschluckt werden
// muss. force: Notbremse, falls ein Loslassen nie ankommt.
func maybeUnhook(force bool) {
	if keyHook.handle == 0 {
		return
	}
	if !keyHook.c.finished() && !force {
		return
	}
	procUnhookWindowsHookEx.Call(keyHook.handle)
	keyHook.handle = 0
	keyHook.c = keyCapture{}
}

// hookStuck: Aufnahme vorbei, aber der Hook hängt noch länger als ein paar
// Sekunden. Der Tray prüft das alle zwei Sekunden.
func hookStuck() bool {
	return keyHook.handle != 0 && !keyHook.c.active && time.Since(keyHook.endedAt) > 3*time.Second
}

// lowLevelKeyboardProc bekommt jeden Tastendruck des Systems, solange der
// Hook hängt. Muss schnell sein: Windows wartet darauf.
func lowLevelKeyboardProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) >= 0 && keyHook.handle != 0 {
		kb := *(**kbdLLHookStruct)(unsafe.Pointer(&lParam))
		if kb.flags&llkhfInjected == 0 { // eigene SendInput-Tasten und fremde Makros nicht
			down := wParam == wmKeyDown || wParam == wmSysKeyDown
			swallow, ev, h := keyHook.c.feed(kb.vkCode, down)
			switch ev {
			case captureDone:
				procPostMessageW.Call(keyHook.notify, wmCaptured, uintptr(h.Mods), uintptr(h.VK))
			case captureCancelled:
				procPostMessageW.Call(keyHook.notify, wmCaptureCancelled, 0, 0)
			}
			if keyHook.c.finished() {
				procPostMessageW.Call(keyHook.tray, wmUnhook, 0, 0)
			}
			if swallow {
				return 1
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

// captured prüft die aufgenommene Taste und speichert sie.
func (s *setupWindow) captured(h hotkey) {
	t := s.captureTarget
	replace := s.replace
	s.endCapture()
	s.t.logger.Info("captured", "hotkey", h.String(), "target", t.String())

	for i, b := range s.t.bindings {
		if b.Hotkey != h || i == replace {
			continue
		}
		if b.Target == t {
			s.setPrompt(h.String() + " springt schon auf " + t.String() + ". Nichts geändert.")
			return
		}
		if !s.ask(fmt.Sprintf("%s springt bisher auf %s.\n\nSoll sie stattdessen auf %s springen?", h, b.Target, t)) {
			s.setPrompt("Nichts geändert.")
			return
		}
	}
	if !s.ownsHotkey(h) && !hotkeyFree(s.hwnd, h) {
		s.setPrompt(h.String() + " ist schon von Windows oder einem anderen Programm belegt. Bitte eine andere nehmen.")
		return
	}
	if h.blocksTyping() && !s.ask(fmt.Sprintf("%s ohne Strg, Alt oder Win fehlt dir danach in allen Programmen, auch beim Schreiben.\n\nTrotzdem nehmen?", h)) {
		s.setPrompt("Nichts geändert. Tipp: mit Strg, Alt oder Win davor stört sie nirgends.")
		return
	}

	list, before := assignBinding(s.t.bindings, h, t, replace)
	if err := s.t.setBindings(list); err != nil {
		s.setPrompt(errorText(err))
		return
	}
	s.fill()
	msg := "Gespeichert: " + h.String() + " springt auf " + t.String() + "."
	if before != nil {
		msg += " (vorher " + before.String() + ")"
	}
	s.setPrompt(msg)
}

// ownsHotkey: die Taste gehört schon Bosskey (dann sagt RegisterHotKey
// "belegt", obwohl nur wir selbst sie haben).
func (s *setupWindow) ownsHotkey(h hotkey) bool {
	for _, b := range s.t.bindings {
		if b.Hotkey == h {
			return true
		}
	}
	return false
}

// hotkeyFree probiert aus, ob Windows die Taste hergibt.
func hotkeyFree(hwnd uintptr, h hotkey) bool {
	r, _, _ := procRegisterHotKey.Call(hwnd, testHotkeyID, uintptr(h.Mods|modNoRepeat), uintptr(h.VK))
	if r == 0 {
		return false
	}
	procUnregisterHotKey.Call(hwnd, testHotkeyID)
	return true
}

func (s *setupWindow) ask(text string) bool {
	body, title := utf16Ptr(text), utf16Ptr(appName)
	r, _, _ := procMessageBoxW.Call(s.hwnd, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), mbYesNo|mbIconQuestion)
	return r == idYes
}

func errorText(err error) string {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue.Msg + ". " + ue.Hint
	}
	return err.Error()
}

func (s *setupWindow) removeSelected() {
	i := s.selectedIndex()
	if i < 0 {
		s.setPrompt("Erst einen Eintrag in der Liste anklicken, dann „Entfernen“.")
		return
	}
	b := s.t.bindings[i]
	if err := s.t.setBindings(removeBinding(s.t.bindings, i)); err != nil {
		s.setPrompt(errorText(err))
		return
	}
	s.fill()
	msg := "Entfernt: " + b.Hotkey.String() + "."
	if len(s.t.bindings) == 0 {
		msg += " Jetzt ist keine Taste mehr eingestellt."
	}
	s.setPrompt(msg)
}

// ---- Fensterfunktion --------------------------------------------------------

func setupWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	s := theSetup
	if s == nil || hwnd != s.hwnd {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch msg {
	case wmCommand:
		id, code := wParam&0xFFFF, (wParam>>16)&0xFFFF
		switch {
		case id == idRecord || id == idOK:
			if s.capturing {
				s.endCapture()
				s.setPrompt("Abgebrochen.")
			} else {
				s.startCapture(-1, s.selectedTarget())
			}
		case id == idRemove:
			s.removeSelected()
		case id == idList && code == lbnDblClk:
			if i := s.selectedIndex(); i >= 0 && !s.capturing {
				s.startCapture(i, s.t.bindings[i].Target)
			}
		case id == idClose || id == idCancel:
			procDestroyWindow.Call(s.hwnd)
		}
		return 0
	case wmCaptured:
		if s.capturing {
			s.captured(hotkey{Mods: uint32(wParam), VK: uint32(lParam)})
		}
		return 0
	case wmCaptureCancelled:
		if s.capturing {
			s.endCapture()
			s.setPrompt("Abgebrochen.")
		}
		return 0
	case wmTimer:
		if wParam == timerCaptureTimeout && s.capturing {
			s.endCapture()
			s.setPrompt("Keine Taste gedrückt, abgebrochen. Nochmal auf „Taste aufnehmen“ klicken.")
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(s.hwnd)
		return 0
	case wmDestroy:
		if s.capturing {
			s.endCapture()
		}
		keyHook.notify = 0
		theSetup = nil
		s.t.logger.Info("setup window closed")
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// setupDialogMessage gibt Tab, Enter und Esc an das Fenster, wie bei einem
// Dialog. Für die Nachrichtenschleife des Trays.
func setupDialogMessage(m *winMsg) bool {
	if theSetup == nil {
		return false
	}
	r, _, _ := procIsDialogMessageW.Call(theSetup.hwnd, uintptr(unsafe.Pointer(m)))
	return r != 0
}
