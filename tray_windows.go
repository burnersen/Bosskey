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
	menuSetup      = 9
	menuOpenConfig = 10
	menuReload     = 11
	menuOpenLog    = 12
	menuAutostart  = 13
	menuQuit       = 14
	menuAnimation  = 15
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
	bindings []binding           // aus tasten.json, sortiert
	keys     map[uintptr]binding // registrierte Hotkeys nach ID
	anim     animationState

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
	keys, err := loadBindings(bindingsPath(t.paths), loaded.Legacy)
	if err != nil {
		return err
	}
	t.startup(loaded, keys)
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
		if setupDialogMessage(&m) {
			continue
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
		// Während einer Aufnahme gehört die Tastatur dem Fenster.
		if b, ok := t.keys[wParam]; ok && keyHook.handle == 0 {
			t.requestJump(b)
		}
		return 0
	case msg == trayCallback:
		switch lParam & 0xFFFF {
		case wmLButtonUp:
			t.openSetup(false)
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		}
		return 0
	case msg == wmUnhook:
		maybeUnhook(false)
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
	for _, b := range t.bindings {
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
	if hookStuck() {
		t.logger.Warn("keyboard hook removed by safety timer")
		maybeUnhook(true)
	}
	if stampOf(t.paths.ConfigFile) != t.cfgStamp {
		t.logger.Info("config file changed")
		t.reload()
	}
}

func (t *tray) reload() {
	loaded, err := loadConfig(t.paths.ConfigFile)
	if err == nil {
		var keys bindingsResult
		keys, err = loadBindings(bindingsPath(t.paths), loaded.Legacy)
		if err == nil {
			t.applyConfig(loaded)
			t.bindings = sortedBindings(keys.Bindings)
			problems := append(loaded.Warnings, keys.Warnings...)
			active, regProblems := t.registerAll()
			t.announce(append(problems, regProblems...), "Einstellungen übernommen", active)
			if theSetup != nil {
				theSetup.fill()
			}
			return
		}
	}
	t.logger.Error("reload config", "err", err)
	t.cfgStamp = stampOf(t.paths.ConfigFile) // nicht alle zwei Sekunden dieselbe Meldung
	t.notify("Einstellungen nicht gelesen", errorText(err)+" Die bisherigen Tasten gelten weiter.", true)
}

// startup: Einstellungen, Tasten und Animationen beim Start, und eine
// Meldung, die alles zusammenfasst (mehrere Sprechblasen hintereinander
// würden sich gegenseitig verdrängen).
func (t *tray) startup(loaded configLoad, keys bindingsResult) {
	t.applyConfig(loaded)
	t.bindings = sortedBindings(keys.Bindings)
	problems := append(append([]string{}, loaded.Warnings...), keys.Warnings...)
	active, regProblems := t.registerAll()
	problems = append(problems, regProblems...)

	var notes []string
	if keys.Migrated > 0 {
		notes = append(notes, fmt.Sprintf("%d Taste(n) aus Bosskey.ini übernommen. Ab jetzt: Linksklick aufs Symbol.", keys.Migrated))
		t.logger.Info("bindings migrated from ini", "count", keys.Migrated)
	}
	if note := t.startAnimations(); note != "" {
		notes = append(notes, note)
	}

	switch {
	case len(problems) > 0:
		t.announce(problems, "", nil)
	case len(notes) > 0:
		t.notify("Bosskey läuft", strings.Join(notes, "\n"), false)
	case t.cfg.ShowStartMessage:
		t.notify("Bosskey läuft", strings.Join(active, "\n"), false)
	}
	if keys.Created {
		t.openSetup(true) // erster Start: gleich zeigen, wie es geht
	}
}

// applyConfig übernimmt die INI.
func (t *tray) applyConfig(loaded configLoad) {
	t.cfg = loaded.Config
	t.cfgStamp = stampOf(t.paths.ConfigFile)
	if loaded.Created {
		t.logger.Info("config created", "path", t.paths.ConfigFile)
	}
	for _, k := range loaded.Added {
		t.logger.Info("config key added", "key", k)
	}
}

// registerAll meldet die alten Hotkeys ab und t.bindings neu an.
func (t *tray) registerAll() (active, problems []string) {
	for id := range t.keys {
		procUnregisterHotKey.Call(t.hwnd, id)
	}
	t.keys = map[uintptr]binding{}
	for i, b := range t.bindings {
		id := uintptr(i + 1)
		r, _, err := procRegisterHotKey.Call(t.hwnd, id, uintptr(b.Hotkey.Mods|modNoRepeat), uintptr(b.Hotkey.VK))
		if r == 0 {
			reason := "geht nicht"
			if err == syscall.Errno(errorHotkeyAlreadyRegistered) {
				reason = "ist schon von Windows oder einem anderen Programm belegt"
			}
			problems = append(problems, fmt.Sprintf("%s %s. Linksklick aufs Symbol, dann eine andere Taste aufnehmen.", b.Hotkey, reason))
			t.logger.Warn("hotkey not registered", "hotkey", b.Hotkey.String(), "err", err)
			continue
		}
		t.keys[id] = b
		active = append(active, b.Hotkey.String()+" = "+b.Target.String())
		t.logger.Info("hotkey registered", "hotkey", b.Hotkey.String(), "target", b.Target.String())
	}
	if len(t.bindings) == 0 {
		problems = append(problems, "Keine Taste eingestellt. Linksklick aufs Symbol, dann eine Taste aufnehmen.")
	}
	t.updateTooltip()
	return active, problems
}

// setBindings speichert neue Tasten aus dem Fenster und meldet sie an.
func (t *tray) setBindings(list []binding) error {
	if err := writeBindings(bindingsPath(t.paths), list); err != nil {
		t.logger.Error("save bindings", "err", err)
		return err
	}
	t.bindings = sortedBindings(list)
	_, problems := t.registerAll()
	for _, p := range problems {
		t.logger.Warn("bindings", "problem", p)
	}
	t.logger.Info("bindings saved", "count", len(list))
	return nil
}

// announce zeigt Probleme als Warnung, sonst (wenn title gesetzt ist) was gilt.
func (t *tray) announce(problems []string, title string, active []string) {
	for _, p := range problems {
		t.logger.Warn("config", "problem", p)
	}
	switch {
	case len(problems) > 0:
		text := problems[0]
		if len(problems) > 1 {
			text += fmt.Sprintf(" (und %d weitere, siehe Protokoll)", len(problems)-1)
		}
		t.notify("Bosskey: bitte prüfen", text, true)
	case title != "":
		t.notify(title, strings.Join(active, "\n"), false)
	}
}

// ---- Animationen ---------------------------------------------------------------

// startAnimations sorgt beim Start dafür, dass der Haken "Desktop-Wechsel
// ohne Animation" gilt. Gibt den Satz für die Startmeldung zurück, wenn
// sich etwas geändert hat oder es nicht ging.
func (t *tray) startAnimations() string {
	path := animationPath(t.paths)
	st, err := readAnimationState(path)
	if err != nil {
		t.logger.Warn("animation state", "err", err)
		return ""
	}
	t.anim = st
	if !st.Hide {
		return ""
	}
	return t.hideAnimations()
}

// hideAnimations schaltet die Animationen ab und sagt, was passiert ist.
func (t *tray) hideAnimations() string {
	cur, err := getAnimations()
	if err != nil {
		t.logger.Warn("read animations", "err", err)
		return "Konnte die Windows-Animationen nicht abschalten. " + animationTip
	}
	plan := planHide(t.anim, cur)
	if plan.Set {
		if err := setAnimations(plan.To); err != nil {
			t.logger.Warn("set animations", "err", err)
			return "Konnte die Windows-Animationen nicht abschalten. " + animationTip
		}
	}
	t.saveAnimationState(plan.State)
	if !plan.Changed {
		return ""
	}
	t.logger.Info("animations off", "before", fmt.Sprintf("%+v", *plan.State.Before))
	return "Windows-Animationen aus, damit man den Desktop-Wechsel nicht sieht. Rein optisch, zurück per Rechtsklick > Desktop-Wechsel ohne Animation."
}

func (t *tray) restoreAnimations() string {
	plan := planRestore(t.anim)
	if plan.Set {
		if err := setAnimations(plan.To); err != nil {
			t.logger.Warn("restore animations", "err", err)
			return "Konnte die Windows-Animationen nicht zurückstellen: " + err.Error()
		}
		t.logger.Info("animations restored", "to", fmt.Sprintf("%+v", plan.To))
	}
	t.saveAnimationState(plan.State)
	return "Windows-Animationen sind wieder so wie vor Bosskey."
}

func (t *tray) saveAnimationState(s animationState) {
	t.anim = s
	if err := writeAnimationState(animationPath(t.paths), s); err != nil {
		t.logger.Warn("save animation state", "err", err)
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
	add(mfString, menuSetup, "Tasten einrichten ...")
	add(mfSeparator, 0, "")
	for i, b := range t.bindings {
		flags := uintptr(mfString)
		label := "Zu " + b.Target.String()
		if b.Target.Back {
			label = "Zurück zum vorigen Desktop"
		}
		add(flags, uintptr(menuBinding+i), label+"\t"+b.Hotkey.String())
	}
	if len(t.bindings) > 0 {
		add(mfSeparator, 0, "")
	}
	anim := uintptr(mfString)
	if t.anim.Hide {
		anim |= mfChecked
	}
	add(anim, menuAnimation, "Desktop-Wechsel ohne Animation")
	add(mfString, menuOpenConfig, "Weitere Einstellungen (INI)")
	add(mfString, menuReload, "Einstellungen neu laden")
	add(mfString, menuOpenLog, "Protokoll öffnen")
	auto := uintptr(mfString)
	if autostartEnabled() {
		auto |= mfChecked
	}
	add(auto, menuAutostart, "Mit Windows starten")
	add(mfSeparator, 0, "")
	add(mfString, menuQuit, "Beenden")

	procSetMenuDefaultItem.Call(menu, menuSetup, 0) // fett: dasselbe wie Linksklick
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
	case cmd == menuSetup:
		t.openSetup(false)
	case cmd == menuAnimation:
		var note string
		if t.anim.Hide {
			note = t.restoreAnimations()
		} else {
			t.anim.Hide = true
			if note = t.hideAnimations(); note == "" {
				note = "Die Windows-Animationen waren schon aus."
			}
		}
		t.notify("Desktop-Wechsel ohne Animation", note, false)
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
	case cmd >= menuBinding && cmd < menuBinding+len(t.bindings):
		t.requestJump(t.bindings[cmd-menuBinding])
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
	if theSetup != nil {
		procDestroyWindow.Call(theSetup.hwnd)
	}
	maybeUnhook(true)
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
