// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Das Symbol im Infobereich, sein Menü, die Hotkeys und die Nachrichten-
// schleife. Ein unsichtbares Fenster empfängt alles, was Windows meldet:
// Hotkeys, Klicks aufs Symbol, Neustart des Explorers.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// trayOptions kommt aus run.
type trayOptions struct {
	paths  appPaths
	logger *slog.Logger
	probe  bool // nach probeDuration von selbst beenden (CI)
}

const (
	trayCallback = wmApp + 1 // Klicks aufs Symbol kommen als diese Nachricht
	trayIconID   = 1

	timerWatch = 1 // prüft die INI auf Änderungen und das Symbol auf Vorhandensein
	timerProbe = 2 // beendet den Probelauf
	// watchEvery: so schnell gelten gespeicherte Änderungen. Kostet nur ein
	// os.Stat je Durchgang.
	watchEvery    = 2000 // Millisekunden
	probeDuration = 3000 // Millisekunden

	// Menüpunkte. Die Tasten bekommen menuBinding + Index.
	menuOpenConfig = 10
	menuReload     = 11
	menuOpenLog    = 12
	menuAutostart  = 13
	menuQuit       = 14
	menuBinding    = 100

	// balloonMax: Windows zeigt höchstens 255 Zeichen in der Sprechblase.
	balloonMax = 250
)

// tray ist der Zustand des laufenden Programms. Es gibt genau einen; die
// Fensterfunktion von Windows erreicht ihn über theTray.
type tray struct {
	logger *slog.Logger
	paths  appPaths
	probe  bool

	hwnd           uintptr
	icon           uintptr
	taskbarCreated uintptr // Nachricht, die der Explorer nach seinem Neustart schickt

	cfg      config
	cfgStamp fileStamp
	keys     map[uintptr]binding // registrierte Hotkeys nach ID

	// iconMu schützt das Symbol: die Sprechblasen kommen auch aus dem
	// Wechsel-Goroutine.
	iconMu    sync.Mutex
	iconAdded bool
	pending   *balloon // Sprechblase, die wartet, bis das Symbol da ist

	jumps chan jumpRequest
}

var theTray *tray

type balloon struct {
	title, text string
	warning     bool
}

// jumpRequest ist ein gedrückter Hotkey. Er trägt die Einstellung mit,
// damit das Wechsel-Goroutine nie auf t.cfg zugreift (die ändert sich beim
// Neuladen im anderen Thread).
type jumpRequest struct {
	b      binding
	toggle bool
}

func runTray(opts trayOptions) error {
	// Fenster, Hotkeys und Nachrichtenschleife hängen an einem Thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	name := utf16Ptr("Local\\" + instanceID)
	mutex, _, lastErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if mutex != 0 && lastErr == syscall.Errno(errorAlreadyExists) {
		return userError(nil, appName+" läuft schon.",
			"Das Symbol sitzt unten rechts im Infobereich, eventuell hinter dem kleinen Pfeil ^.")
	}
	// Der Mutex bleibt offen, bis der Prozess endet: das ist die Sperre.

	t := &tray{
		logger: opts.logger,
		paths:  opts.paths,
		probe:  opts.probe,
		keys:   map[uintptr]binding{},
		jumps:  make(chan jumpRequest, 1),
	}
	theTray = t
	if err := t.createWindow(); err != nil {
		return userError(err, appName+" kann sein unsichtbares Fenster nicht anlegen", "Einzelheiten stehen im Log. Einfach neu starten.")
	}
	t.icon = loadAppIcon()
	t.addIcon()

	loaded, err := loadConfig(t.paths.ConfigFile)
	if err != nil {
		return err
	}
	t.applyConfig(loaded, true)

	if st, err := readDesktops(); err != nil {
		t.logger.Warn("read desktops", "err", err)
	} else {
		t.logger.Info("desktops", "state", describeDesktops(st), "build", windowsBuild())
	}

	procSetTimer.Call(t.hwnd, timerWatch, watchEvery, 0)
	if t.probe {
		procSetTimer.Call(t.hwnd, timerProbe, probeDuration, 0)
	}
	go t.jumpWorker()

	var m winMsg
	for {
		r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			t.cleanup()
			return fmt.Errorf("GetMessage: %w", err)
		}
		if r == 0 { // WM_QUIT
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	t.cleanup()
	return nil
}

func (t *tray) createWindow() error {
	className := utf16Ptr("BosskeyTrayWindow")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hinst,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	// Ein normales, nie gezeigtes Fenster, kein reines Nachrichtenfenster:
	// nur normale Fenster bekommen "TaskbarCreated" nach einem Explorer-Neustart.
	title := utf16Ptr(appName)
	h, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, hinst, 0)
	if h == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	t.hwnd = h
	tc := utf16Ptr("TaskbarCreated")
	t.taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tc)))
	return nil
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	t := theTray
	if t == nil || hwnd != t.hwnd {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch {
	case msg == wmHotkey:
		if b, ok := t.keys[wParam]; ok {
			t.requestJump(b)
		}
		return 0
	case msg == trayCallback:
		switch lParam & 0xFFFF {
		case wmRButtonUp, wmLButtonUp, wmContextMenu:
			t.showMenu()
		}
		return 0
	case msg == wmTimer && wParam == timerWatch:
		t.watch()
		return 0
	case msg == wmTimer && wParam == timerProbe:
		t.logger.Info("probe finished")
		procDestroyWindow.Call(t.hwnd)
		return 0
	case msg == wmClose:
		procDestroyWindow.Call(t.hwnd)
		return 0
	case msg == wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	case t.taskbarCreated != 0 && msg == t.taskbarCreated:
		// Explorer wurde neu gestartet, alle Symbole sind weg.
		t.logger.Info("taskbar recreated")
		t.iconMu.Lock()
		t.iconAdded = false
		t.iconMu.Unlock()
		t.addIcon()
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// ---- Symbol und Sprechblasen ------------------------------------------------

// loadAppIcon nimmt das Symbol aus der exe (vom Release-Bau mit go-winres
// eingebettet), sonst das Standardsymbol von Windows.
func loadAppIcon() uintptr {
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	cy, _, _ := procGetSystemMetrics.Call(smCySmIcon)
	name := utf16Ptr("APP")
	if h, _, _ := procLoadImageW.Call(hinst, uintptr(unsafe.Pointer(name)), imageIcon, cx, cy, lrDefaultColor); h != 0 {
		return h
	}
	h, _, _ := procLoadIconW.Call(0, idiApplication)
	return h
}

func (t *tray) iconData() notifyIconData {
	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = t.hwnd
	nid.uID = trayIconID
	return nid
}

// addIcon setzt das Symbol in den Infobereich. Klappt das nicht (beim
// Autostart ist der Explorer manchmal noch nicht so weit), versucht es
// watch alle zwei Sekunden wieder.
func (t *tray) addIcon() {
	t.iconMu.Lock()
	defer t.iconMu.Unlock()
	if t.iconAdded {
		return
	}
	nid := t.iconData()
	nid.uFlags = nifMessage | nifIcon | nifTip
	nid.uCallbackMessage = trayCallback
	nid.hIcon = t.icon
	copyUTF16(nid.szTip[:], t.tooltip())
	if r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		t.logger.Debug("tray icon not added yet")
		return
	}
	t.iconAdded = true
	t.logger.Info("tray icon added")
	if t.pending != nil {
		b := *t.pending
		t.pending = nil
		t.showBalloonLocked(b)
	}
}

func (t *tray) updateTooltip() {
	t.iconMu.Lock()
	defer t.iconMu.Unlock()
	if !t.iconAdded {
		return
	}
	nid := t.iconData()
	nid.uFlags = nifTip
	copyUTF16(nid.szTip[:], t.tooltip())
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// tooltip zeigt beim Drüberfahren die ersten Tasten. Windows schneidet
// bei 127 Zeichen ab, das macht copyUTF16 genauso.
func (t *tray) tooltip() string {
	parts := []string{}
	for _, b := range t.cfg.Bindings {
		parts = append(parts, b.Hotkey.String()+" = "+b.Target.String())
	}
	if len(parts) == 0 {
		return appName + ": keine Taste eingestellt"
	}
	return appName + "\n" + strings.Join(parts, "\n")
}

// notify zeigt eine Sprechblase (unter Windows 10 und 11 eine Benachrichtigung).
func (t *tray) notify(title, text string, warning bool) {
	t.iconMu.Lock()
	defer t.iconMu.Unlock()
	b := balloon{title: title, text: text, warning: warning}
	if !t.iconAdded {
		t.pending = &b
		return
	}
	t.showBalloonLocked(b)
}

func (t *tray) showBalloonLocked(b balloon) {
	nid := t.iconData()
	nid.uFlags = nifInfo
	nid.dwInfoFlags = niifInfo
	if b.warning {
		nid.dwInfoFlags = niifWarning
	}
	copyUTF16(nid.szInfoTitle[:], b.title)
	copyUTF16(nid.szInfo[:], shorten(b.text, balloonMax))
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}

// ---- Einstellungen und Hotkeys ----------------------------------------------

// fileStamp erkennt, ob die INI gespeichert wurde.
type fileStamp struct {
	mod  time.Time
	size int64
	ok   bool
}

func stampOf(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{mod: info.ModTime(), size: info.Size(), ok: true}
}

// watch läuft alle zwei Sekunden: Symbol noch da? INI geändert?
func (t *tray) watch() {
	t.addIcon()
	if stampOf(t.paths.ConfigFile) != t.cfgStamp {
		t.logger.Info("config file changed")
		t.reload()
	}
}

func (t *tray) reload() {
	loaded, err := loadConfig(t.paths.ConfigFile)
	if err != nil {
		var msg string
		var ue *UserError
		if errors.As(err, &ue) {
			msg = ue.Msg + ". " + ue.Hint
		} else {
			msg = err.Error()
		}
		t.logger.Error("reload config", "err", err)
		t.cfgStamp = stampOf(t.paths.ConfigFile) // nicht alle zwei Sekunden dieselbe Meldung
		t.notify("Einstellungen nicht gelesen", msg+" Die bisherigen Tasten gelten weiter.", true)
		return
	}
	t.applyConfig(loaded, false)
}

// applyConfig meldet die alten Hotkeys ab und die neuen an und sagt dem
// Nutzer, was gilt und was nicht geht.
func (t *tray) applyConfig(loaded configLoad, first bool) {
	for id := range t.keys {
		procUnregisterHotKey.Call(t.hwnd, id)
	}
	t.keys = map[uintptr]binding{}
	t.cfg = loaded.Config
	t.cfgStamp = stampOf(t.paths.ConfigFile)

	if loaded.Created {
		t.logger.Info("config created", "path", t.paths.ConfigFile)
	}
	for _, k := range loaded.Added {
		t.logger.Info("config key added", "key", k)
	}
	problems := append([]string{}, loaded.Warnings...)
	var active []string
	for i, b := range t.cfg.Bindings {
		id := uintptr(i + 1)
		r, _, err := procRegisterHotKey.Call(t.hwnd, id, uintptr(b.Hotkey.Mods|modNoRepeat), uintptr(b.Hotkey.VK))
		if r == 0 {
			reason := "Windows lässt sie nicht zu"
			if err == syscall.Errno(errorHotkeyAlreadyRegistered) {
				reason = "ist schon von Windows oder einem anderen Programm belegt"
			}
			problems = append(problems, fmt.Sprintf("%s (Zeile %d) %s. Nimm eine andere Taste.", b.Hotkey, b.Line, reason))
			t.logger.Warn("hotkey not registered", "hotkey", b.Hotkey.String(), "line", b.Line, "err", err)
			continue
		}
		t.keys[id] = b
		active = append(active, b.Hotkey.String()+" = "+b.Target.String())
		t.logger.Info("hotkey registered", "hotkey", b.Hotkey.String(), "target", b.Target.String())
	}
	if len(t.cfg.Bindings) == 0 {
		problems = append(problems, "Keine Taste eingestellt. Rechtsklick aufs Symbol > Einstellungen öffnen, unter [Tasten] z. B. \"Pause = 1\" eintragen.")
	}
	for _, p := range problems {
		t.logger.Warn("config", "problem", p)
	}
	t.updateTooltip()

	switch {
	case len(problems) > 0:
		text := problems[0]
		if len(problems) > 1 {
			text += fmt.Sprintf(" (und %d weitere, siehe Protokoll)", len(problems)-1)
		}
		t.notify("Bosskey: Einstellungen prüfen", text, true)
	case first && t.cfg.ShowStartMessage:
		t.notify("Bosskey läuft", strings.Join(active, "\n"), false)
	case !first:
		t.notify("Einstellungen übernommen", strings.Join(active, "\n"), false)
	}
}

// ---- Desktop wechseln --------------------------------------------------------

// requestJump reicht einen Hotkey an das Wechsel-Goroutine weiter. Läuft
// schon ein Wechsel, wird der Druck verworfen: wildes Mehrfachdrücken soll
// sich nicht stapeln.
func (t *tray) requestJump(b binding) {
	select {
	case t.jumps <- jumpRequest{b: b, toggle: t.cfg.ToggleBack}:
	default:
		t.logger.Info("jump ignored, still busy", "hotkey", b.Hotkey.String())
	}
}

// jumpWorker wechselt nacheinander, außerhalb der Nachrichtenschleife:
// ein Wechsel wartet auf Windows, das Symbol soll derweil antworten.
func (t *tray) jumpWorker() {
	previous := noPrevious
	for req := range t.jumps {
		st, err := readDesktops()
		if err != nil {
			t.reportJumpError(userError(err, "Kann die virtuellen Desktops nicht lesen", "Einzelheiten stehen im Log."))
			continue
		}
		current := noPrevious
		if st.Known {
			current = st.Pos
		}
		dest, err := decideDestination(req.b.Target, current, previous, req.toggle)
		if err != nil {
			t.reportJumpError(err)
			continue
		}
		t.logger.Info("jump", "hotkey", req.b.Hotkey.String(), "from", describeDesktops(st), "to", dest+1)
		if st.Known && dest == st.Pos {
			continue // schon da
		}
		if err := switchDesktop(dest, t.logger); err != nil {
			t.reportJumpError(err)
			continue
		}
		if st.Known {
			previous = st.Pos
		}
	}
}

func (t *tray) reportJumpError(err error) {
	var ue *UserError
	if errors.As(err, &ue) {
		t.logger.Warn(ue.Msg, causeAttr(ue.Err)...)
		t.notify(ue.Msg, ue.Hint, true)
		return
	}
	t.logger.Error("jump failed", "err", err)
	t.notify("Wechsel gescheitert", err.Error(), true)
}

// ---- Menü --------------------------------------------------------------------

func (t *tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	add := func(flags, id uintptr, text string) {
		p := utf16Ptr(text)
		procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
	}
	add(mfString|mfGrayed, 0, appName+" "+version)
	add(mfSeparator, 0, "")
	for i, b := range t.cfg.Bindings {
		flags := uintptr(mfString)
		label := "Zu " + b.Target.String()
		if b.Target.Back {
			label = "Zurück zum vorigen Desktop"
		}
		add(flags, uintptr(menuBinding+i), label+"\t"+b.Hotkey.String())
	}
	if len(t.cfg.Bindings) > 0 {
		add(mfSeparator, 0, "")
	}
	add(mfString, menuOpenConfig, "Einstellungen öffnen")
	add(mfString, menuReload, "Einstellungen neu laden")
	add(mfString, menuOpenLog, "Protokoll öffnen")
	auto := uintptr(mfString)
	if autostartEnabled() {
		auto |= mfChecked
	}
	add(auto, menuAutostart, "Mit Windows starten")
	add(mfSeparator, 0, "")
	add(mfString, menuQuit, "Beenden")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Ohne SetForegroundWindow geht das Menü nicht zu, wenn man daneben
	// klickt; das WM_NULL danach ist der dokumentierte zweite Teil davon.
	procSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmBottomAlign,
		uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	t.menuCommand(int(cmd))
}

func (t *tray) menuCommand(cmd int) {
	switch {
	case cmd == menuOpenConfig:
		if _, err := os.Stat(t.paths.ConfigFile); err != nil {
			t.reload() // legt die Datei mit den Standardwerten an
		}
		t.open(t.paths.ConfigFile)
	case cmd == menuReload:
		t.reload()
	case cmd == menuOpenLog:
		t.open(t.paths.LogFile)
	case cmd == menuAutostart:
		on := !autostartEnabled()
		if err := setAutostart(on); err != nil {
			t.logger.Error("autostart", "on", on, "err", err)
			t.notify("Autostart nicht geändert", err.Error(), true)
			return
		}
		t.logger.Info("autostart", "on", on)
	case cmd == menuQuit:
		t.logger.Info("quit by user")
		procDestroyWindow.Call(t.hwnd)
	case cmd >= menuBinding && cmd < menuBinding+len(t.cfg.Bindings):
		t.requestJump(t.cfg.Bindings[cmd-menuBinding])
	}
}

// open öffnet eine Datei mit dem Programm, das Windows dafür eingestellt
// hat; gibt es keins (.log ist oft nicht verknüpft), mit dem Editor.
func (t *tray) open(path string) {
	verb, file := utf16Ptr("open"), utf16Ptr(path)
	r, _, _ := procShellExecuteW.Call(t.hwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, swShowNormal)
	if r > 32 { // ShellExecute: Werte über 32 heißen Erfolg
		return
	}
	notepad, arg := utf16Ptr("notepad.exe"), utf16Ptr(`"`+path+`"`)
	r, _, _ = procShellExecuteW.Call(t.hwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(notepad)), uintptr(unsafe.Pointer(arg)), 0, swShowNormal)
	if r <= 32 {
		t.logger.Warn("open file", "path", path, "code", r)
		t.notify("Kann die Datei nicht öffnen", path, true)
	}
}

func (t *tray) cleanup() {
	for id := range t.keys {
		procUnregisterHotKey.Call(t.hwnd, id)
	}
	t.iconMu.Lock()
	if t.iconAdded {
		nid := t.iconData()
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		t.iconAdded = false
	}
	t.iconMu.Unlock()
	close(t.jumps)
}
