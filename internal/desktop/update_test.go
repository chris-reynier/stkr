package desktop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/hotkey"
)

// eventLog records the order of the steps of an update.
type eventLog struct {
	mu sync.Mutex
	ev []string
}

func (l *eventLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ev = append(l.ev, s)
}

func (l *eventLog) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.ev, ",")
}

// fakeUpdater stands in for the GitHub release client and the installer.
type fakeUpdater struct {
	log       *eventLog
	info      UpdateInfo
	checkErr  error
	stageErr  error
	commitErr error
	setup     bool          // the staged update is a setup that relaunches itself
	hold      chan struct{} // when set, Stage waits for it
	staged    *fakeStaged
}

func (f *fakeUpdater) Check(context.Context) (UpdateInfo, error) {
	f.log.add("check")
	return f.info, f.checkErr
}

func (f *fakeUpdater) Stage(context.Context) (StagedUpdate, error) {
	f.log.add("stage")
	if f.hold != nil {
		<-f.hold
	}
	if f.stageErr != nil {
		return nil, f.stageErr
	}
	f.staged = &fakeStaged{f: f}
	return f.staged, nil
}

type fakeStaged struct{ f *fakeUpdater }

func (s *fakeStaged) Version() string  { return "v0.3.0" }
func (s *fakeStaged) Relaunches() bool { return s.f.setup }
func (s *fakeStaged) Commit(context.Context) error {
	s.f.log.add("commit")
	return s.f.commitErr
}
func (s *fakeStaged) Discard() { s.f.log.add("discard") }

// loggedHost wraps a host function so the server shutdown shows in the log.
func loggedHost(h *serviceHost, l *eventLog) HostFunc {
	return func(ctx context.Context, port int) (Hosted, error) {
		hd, err := h.start(ctx, port)
		if err != nil {
			return nil, err
		}
		l.add("start")
		return &loggedHosted{Hosted: hd, l: l}, nil
	}
}

type loggedHosted struct {
	Hosted
	l *eventLog
}

func (h *loggedHosted) Shutdown(ctx context.Context) error {
	h.l.add("shutdown")
	return h.Hosted.Shutdown(ctx)
}

// updateApp starts an app that hosts a real server and has the fake updater.
func updateApp(t *testing.T, fu *fakeUpdater, confirm func(string, string) bool, mut func(*Options)) (*runningApp, *eventLog) {
	t.Helper()
	l := &eventLog{}
	fu.log = l
	h, home := testHost(t, "")
	r := startApp2(t, h, home, l, func(o *Options) {
		o.Updater = fu
		o.Confirm = confirm
		o.Exe = `C:\stkr\stkrw.exe`
		if mut != nil {
			mut(o)
		}
	})
	waitFor(t, "hosted", func() bool { return r.app.Mode() == ModeHost && r.tray.lastTip() == "STKR - Nothing running" })
	return r, l
}

func startApp2(t *testing.T, h *serviceHost, home string, l *eventLog, mut func(*Options)) *runningApp {
	t.Helper()
	logs := &syncBuffer{}
	r := &runningApp{tray: newFakeTray(), open: &opener{}, logs: &logs.Buffer, done: make(chan error, 1)}
	opts := Options{
		Home: home, Version: "0.2.0", Port: 0, Poll: 30 * time.Millisecond, Notifications: true,
		Tray: r.tray, Host: loggedHost(h, l), Open: r.open.open, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	}
	mut(&opts)
	r.app = NewApp(opts)
	go func() { r.done <- r.app.Run() }()
	<-r.tray.ready
	return r
}

func waitDone(t *testing.T, r *runningApp) {
	t.Helper()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the app did not quit")
	}
}

func yes(string, string) bool { return true }
func no(string, string) bool  { return false }

func TestUpdateFlowReplacesAndRelaunches(t *testing.T) {
	fu := &fakeUpdater{}
	var asked string
	r, l := updateApp(t, fu, func(title, text string) bool { asked = title + "|" + text; return true }, nil)

	r.app.Select(ActInstallUpdate)
	waitDone(t, r)
	// Download with the server still up, then stop it, then install.
	if got := l.all(); got != "start,stage,shutdown,commit" {
		t.Fatalf("steps = %s", got)
	}
	exe, args := r.app.PendingRelaunch()
	if exe != `C:\stkr\stkrw.exe` || len(args) != 1 || args[0] != AfterUpdateFlag {
		t.Errorf("relaunch = %q %v", exe, args)
	}
	if !strings.Contains(asked, "Install STKR update|") || !strings.Contains(asked, "Your data is kept") {
		t.Errorf("confirmation = %q", asked)
	}
	if r.app.Mode() != ModeNone {
		t.Errorf("mode after the update = %q", r.app.Mode())
	}
	if !strings.Contains(r.tray.noticeTexts(), "Downloading the update") {
		t.Error("no progress notification")
	}
}

func TestUpdateFlowSetupRelaunchesItself(t *testing.T) {
	fu := &fakeUpdater{setup: true}
	r, l := updateApp(t, fu, yes, nil)
	r.app.Select(ActInstallUpdate)
	waitDone(t, r)
	if got := l.all(); got != "start,stage,shutdown,commit" {
		t.Fatalf("steps = %s", got)
	}
	if exe, _ := r.app.PendingRelaunch(); exe != "" {
		t.Errorf("the app relaunches although the setup does it: %q", exe)
	}
}

func TestUpdateConfirmationDeclined(t *testing.T) {
	fu := &fakeUpdater{}
	r, l := updateApp(t, fu, no, nil)
	r.app.Select(ActInstallUpdate)
	time.Sleep(200 * time.Millisecond)
	if got := l.all(); got != "start" {
		t.Fatalf("steps = %s (nothing may happen after No)", got)
	}
	if r.app.Mode() != ModeHost {
		t.Errorf("mode = %q", r.app.Mode())
	}
	// No confirm callback counts as No.
	r2, l2 := updateApp(t, &fakeUpdater{}, nil, nil)
	r2.app.Select(ActInstallUpdate)
	time.Sleep(200 * time.Millisecond)
	if l2.all() != "start" {
		t.Errorf("steps without a confirm callback = %s", l2.all())
	}
	r.quit(t)
	r2.quit(t)
}

func TestUpdateDownloadFailureKeepsServer(t *testing.T) {
	fu := &fakeUpdater{stageErr: errors.New("checksum mismatch")}
	r, l := updateApp(t, fu, yes, nil)
	r.app.Select(ActInstallUpdate)
	waitFor(t, "error notice", func() bool {
		return strings.Contains(r.tray.noticeTexts(), "could not be downloaded: checksum mismatch")
	})
	if got := l.all(); got != "start,stage" {
		t.Fatalf("steps = %s: the server must keep running when the download fails", got)
	}
	if r.app.Mode() != ModeHost {
		t.Errorf("mode = %q", r.app.Mode())
	}
	r.app.Select(ActInstallUpdate) // not stuck busy: it can be tried again
	waitFor(t, "second try", func() bool { return strings.Count(l.all(), "stage") == 2 })
	r.quit(t)
}

func TestUpdateInstallFailureRestartsServer(t *testing.T) {
	fu := &fakeUpdater{commitErr: errors.New("access denied")}
	r, l := updateApp(t, fu, yes, nil)
	r.app.Select(ActInstallUpdate)
	waitFor(t, "error notice", func() bool {
		return strings.Contains(r.tray.noticeTexts(), "the update failed and was not installed (access denied)")
	})
	// stop -> apply (fails, rolled back by the updater) -> server restarts.
	waitFor(t, "server restarted", func() bool { return strings.HasSuffix(l.all(), "start,stage,shutdown,commit,discard,start") })
	waitFor(t, "connected again", func() bool { return r.app.Mode() == ModeHost && r.tray.lastTip() == "STKR - Nothing running" })
	if exe, _ := r.app.PendingRelaunch(); exe != "" {
		t.Error("relaunch requested after a failed update")
	}
	r.quit(t)
}

func TestUpdateRefusedWhenAttached(t *testing.T) {
	f := newFakeServer(t)
	fu := &fakeUpdater{log: &eventLog{}}
	r := startApp2(t, nil, t.TempDir(), fu.log, func(o *Options) {
		o.Port = f.port()
		o.Host = noHost
		o.Updater = fu
		o.Confirm = yes
	})
	waitFor(t, "attach", func() bool { return r.app.Mode() == ModeAttach })
	r.app.Select(ActInstallUpdate)
	waitFor(t, "hint", func() bool {
		return strings.Contains(r.tray.noticeTexts(), "attached to `stkr serve`") && strings.Contains(r.tray.noticeTexts(), "`stkr update`")
	})
	if err := r.app.RequestUpdate(); err == nil || !strings.Contains(err.Error(), "stkr serve") {
		t.Errorf("RequestUpdate when attached: %v", err)
	}
	if fu.log.all() != "" {
		t.Errorf("the updater ran while attached: %s", fu.log.all())
	}
	// "Check for updates" opens the Updates box of the attached server's Settings.
	r.app.Select(ActCheckUpdate)
	waitFor(t, "settings opened", func() bool { return strings.Contains(r.open.all(), "/ui/settings#updates") })
	r.quit(t)
}

func TestRequestUpdateFromTheWebUI(t *testing.T) {
	fu := &fakeUpdater{}
	// The click on the page is the confirmation: no message box.
	r, l := updateApp(t, fu, func(string, string) bool { t.Error("the web request asked for a confirmation"); return false }, nil)
	if err := r.app.RequestUpdate(); err != nil {
		t.Fatal(err)
	}
	waitDone(t, r)
	if got := l.all(); got != "start,stage,shutdown,commit" {
		t.Fatalf("steps = %s", got)
	}
}

func TestRequestUpdateWithoutUpdater(t *testing.T) {
	a := NewApp(Options{Tray: newFakeTray()})
	if err := a.RequestUpdate(); err == nil {
		t.Error("an app without an updater accepted the request")
	}
	if _, err := a.installUpdate(false); err == nil {
		t.Error("install without an updater succeeded")
	}
}

func TestOnlyOneUpdateAtATime(t *testing.T) {
	hold := make(chan struct{})
	fu := &fakeUpdater{hold: hold}
	r, l := updateApp(t, fu, yes, nil)
	if err := r.app.RequestUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "download started", func() bool { return strings.Contains(l.all(), "stage") })
	if err := r.app.RequestUpdate(); !errors.Is(err, errUpdateBusy) {
		t.Errorf("second request: %v", err)
	}
	close(hold)
	waitDone(t, r)
	if strings.Count(l.all(), "stage") != 1 {
		t.Errorf("steps = %s", l.all())
	}
}

func TestCheckForUpdatesNotification(t *testing.T) {
	for name, tc := range map[string]struct {
		fu   fakeUpdater
		want string
	}{
		"available": {fakeUpdater{info: UpdateInfo{Current: "v0.2.0", Latest: "v0.3.0", Available: true}}, "STKR v0.3.0 is available (you have v0.2.0). Choose Install update"},
		"current":   {fakeUpdater{info: UpdateInfo{Current: "v0.2.0", Latest: "v0.2.0"}}, "STKR is up to date (v0.2.0)."},
		"failure":   {fakeUpdater{checkErr: errors.New("no network")}, "could not check for updates: no network"},
	} {
		t.Run(name, func(t *testing.T) {
			fu := tc.fu
			r, _ := updateApp(t, &fu, yes, nil)
			r.app.Select(ActCheckUpdate)
			waitFor(t, "notice", func() bool { return strings.Contains(r.tray.noticeTexts(), tc.want) })
			r.quit(t)
		})
	}
}

func TestAfterUpdateNotificationOnce(t *testing.T) {
	l := &eventLog{}
	h, home := testHost(t, "")
	r := startApp2(t, h, home, l, func(o *Options) { o.JustUpdated = true })
	waitFor(t, "notice", func() bool { return strings.Contains(r.tray.noticeTexts(), "STKR updated to 0.2.0.") })
	time.Sleep(200 * time.Millisecond) // more polls
	if n := strings.Count(r.tray.noticeTexts(), "STKR updated to"); n != 1 {
		t.Errorf("announced %d times", n)
	}
	r.quit(t)
}

// ---- settings apply live --------------------------------------------------

func TestSettingsSavedRestartsHostedServerWithNewConfig(t *testing.T) {
	old := RestartDelay
	RestartDelay = 0
	t.Cleanup(func() { RestartDelay = old })

	l := &eventLog{}
	h, home := testHost(t, "")
	cfg := config.Default()
	cfg.Update.Check = false
	cfg.Port = freePort(t)
	h.cfg = cfg
	if err := config.Save(config.Path(home), cfg); err != nil {
		t.Fatal(err)
	}
	r := startApp2(t, h, home, l, func(o *Options) { o.Port = cfg.Port; o.Reload = h.reload; o.Hotkey = "" })
	waitFor(t, "hosted", func() bool { return r.app.Mode() == ModeHost && r.tray.lastTip() == "STKR - Nothing running" })
	port := r.app.client.Port()
	if port != cfg.Port {
		t.Fatalf("port = %d, want %d", port, cfg.Port)
	}
	h.mu.Lock()
	before := h.svc
	h.mu.Unlock()
	if before.Config().DayEnd != "17:00" {
		t.Fatalf("day_end = %s", before.Config().DayEnd)
	}

	// The settings page saved a new value to the file and asked for a restart.
	saved := *cfg
	saved.DayEnd = "18:30"
	saved.Desktop.Notifications = false
	saved.Desktop.PollSeconds = 7
	if err := config.Save(config.Path(home), &saved); err != nil {
		t.Fatal(err)
	}
	r.app.SettingsSaved()
	waitFor(t, "restart", func() bool { return l.all() == "start,shutdown,start" })
	waitFor(t, "connected again", func() bool { return r.app.Mode() == ModeHost && r.tray.lastTip() == "STKR - Nothing running" })

	if got := r.app.client.Port(); got != port {
		t.Errorf("port after the restart = %d, want the same %d", got, port)
	}
	h.mu.Lock()
	after := h.svc
	h.mu.Unlock()
	if after == before || after.Config().DayEnd != "18:30" {
		t.Fatalf("the restarted server does not use the new config (day_end %s)", after.Config().DayEnd)
	}
	if st, err := NewClient(port).Status(context.Background()); err != nil || st.HostedBy != "desktop" {
		t.Fatalf("status after restart: %+v %v", st, err)
	}
	r.app.mu.Lock()
	notif, poll := r.app.opts.Notifications, r.app.opts.Poll
	r.app.mu.Unlock()
	if notif || poll != 7*time.Second {
		t.Errorf("desktop settings not applied: notifications=%v poll=%v", notif, poll)
	}
	r.quit(t)
}

func TestSettingsSavedPortChange(t *testing.T) {
	old := RestartDelay
	RestartDelay = 0
	t.Cleanup(func() { RestartDelay = old })
	l := &eventLog{}
	h, home := testHost(t, "")
	cfg := config.Default()
	cfg.Update.Check = false
	cfg.Port = freePort(t)
	h.cfg = cfg
	r := startApp2(t, h, home, l, func(o *Options) { o.Port = cfg.Port; o.Reload = h.reload })
	waitFor(t, "hosted", func() bool { return r.app.Mode() == ModeHost })
	newPort := freePort(t)
	saved := *cfg
	saved.Port = newPort
	if err := config.Save(config.Path(home), &saved); err != nil {
		t.Fatal(err)
	}
	r.app.SettingsSaved()
	waitFor(t, "new port", func() bool { return r.app.Mode() == ModeHost && r.app.client.Port() == newPort })
	r.quit(t)
}

func TestSettingsSavedBadFileKeepsRunning(t *testing.T) {
	old := RestartDelay
	RestartDelay = 0
	t.Cleanup(func() { RestartDelay = old })
	l := &eventLog{}
	h, home := testHost(t, "")
	r := startApp2(t, h, home, l, func(o *Options) { o.Reload = h.reload })
	waitFor(t, "hosted", func() bool { return r.app.Mode() == ModeHost })
	// No config file in this home: loading gives defaults, so break the file.
	if err := os.WriteFile(config.Path(home), []byte("port: [not a number"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.app.SettingsSaved()
	waitFor(t, "error", func() bool { return strings.Contains(r.tray.noticeTexts(), "saved settings could not be applied") })
	// The server comes back with its previous configuration.
	waitFor(t, "running again", func() bool { return r.app.Mode() == ModeHost && r.tray.lastTip() == "STKR - Nothing running" })
	r.quit(t)
}

func TestSettingsSavedIgnoredWhenAttached(t *testing.T) {
	old := RestartDelay
	RestartDelay = 0
	t.Cleanup(func() { RestartDelay = old })
	f := newFakeServer(t)
	l := &eventLog{}
	r := startApp2(t, nil, t.TempDir(), l, func(o *Options) { o.Port = f.port(); o.Host = noHost })
	waitFor(t, "attach", func() bool { return r.app.Mode() == ModeAttach })
	r.app.SettingsSaved()
	time.Sleep(200 * time.Millisecond)
	if r.app.Mode() != ModeAttach {
		t.Errorf("mode = %q: nothing may be restarted when attached", r.app.Mode())
	}
	r.quit(t)
}

func TestApplySettingsHotkeyAndPin(t *testing.T) {
	fw := &fakeWindows{}
	r, _ := startAppWith(t, newFakeTray(), func(o *Options) {
		o.Windows = fw.factory()
		o.PanelOnTop = true
		o.Hotkey = "Ctrl+Alt+Space"
	})
	waitFor(t, "first hotkey", func() bool { r.tray.mu.Lock(); defer r.tray.mu.Unlock(); return len(r.tray.hotkeys) == 1 })
	r.app.applySettings(Settings{Port: 1, Poll: time.Second, Notifications: true, PanelOnTop: false, Hotkey: "Ctrl+Shift+K"}, 9999)
	r.tray.mu.Lock()
	keys := append([]hotkey.Key(nil), r.tray.hotkeys...)
	r.tray.mu.Unlock()
	if len(keys) != 2 || keys[1].String() != "Ctrl+Shift+K" {
		t.Errorf("hotkeys = %v", keys)
	}
	if !strings.Contains(fw.all(), "on-top false") {
		t.Errorf("pin not applied: %s", fw.all())
	}
	// Turning the hotkey off unregisters the old one.
	r.app.applySettings(Settings{Port: 1, Poll: time.Second, Hotkey: ""}, 9999)
	r.tray.mu.Lock()
	n := len(r.tray.hotkeys)
	last := r.tray.hotkeys[n-1]
	r.tray.mu.Unlock()
	if n != 3 || !last.IsZero() {
		t.Errorf("hotkeys after turning off = %v", r.tray.hotkeys)
	}
	// The port stays where the server runs unless the saved port changed.
	r.app.mu.Lock()
	port := r.app.opts.Port
	r.app.mu.Unlock()
	if port != 9999 {
		t.Errorf("port = %d, want the running port kept", port)
	}
	r.quit(t)
}

// handlerTray is a fake tray that keeps the handler, so a test can press
// menu items like the user would.
type handlerTray struct {
	*fakeTray
	h chan Handler
}

func (t *handlerTray) Run(h Handler) error {
	t.h <- h
	return t.fakeTray.Run(h)
}

// TestMainStartsTheNewProgramAfterTheLockIsReleased runs the whole program:
// an update through the menu makes Main start stkrw again with --after-update,
// and only after the single-instance lock was released (the new instance
// would otherwise find the old one and exit).
func TestMainStartsTheNewProgramAfterTheLockIsReleased(t *testing.T) {
	tray := &handlerTray{fakeTray: newFakeTray(), h: make(chan Handler, 1)}
	hooks(t, tray, false, nil)
	l := &eventLog{}
	acquireInstance = func(string) (func(), bool, error) { return func() { l.add("release") }, false, nil }
	confirmBox = yes
	fu := &fakeUpdater{log: l}
	newUpdater = func(*serviceHost, string) UpdateOps { return fu }
	var gotExe string
	var gotArgs []string
	relaunchProgram = func(_ context.Context, exe string, args []string) error {
		l.add("relaunch")
		gotExe, gotArgs = exe, args
		return nil
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Port = freePort(t)
	cfg.Update.Check = false
	if err := config.Save(config.Path(home), cfg); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { done <- Main([]string{"-home", home}, &bytes.Buffer{}, "0.2.0") }()
	h := <-tray.h
	waitFor(t, "hosted server", func() bool { _, err := NewClient(cfg.Port).Status(context.Background()); return err == nil })
	h.Select(ActInstallUpdate)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Main did not return")
	}
	if got := l.all(); got != "stage,commit,release,relaunch" {
		t.Fatalf("order = %s", got)
	}
	if !strings.HasSuffix(gotExe, "stkrw.exe") || len(gotArgs) != 1 || gotArgs[0] != "--after-update" {
		t.Errorf("relaunch %q %v", gotExe, gotArgs)
	}
	// The server is down before the program is replaced.
	if _, err := NewClient(cfg.Port).Status(context.Background()); err == nil {
		t.Error("the server is still running after the update")
	}
}

func TestMainAfterUpdateFlag(t *testing.T) {
	tray := newFakeTray()
	hooks(t, tray, false, nil)
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Port = freePort(t)
	cfg.Update.Check = false
	if err := config.Save(config.Path(home), cfg); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { done <- Main([]string{"-home", home, "--after-update"}, &bytes.Buffer{}, "0.3.0") }()
	<-tray.ready
	waitFor(t, "notice", func() bool { return strings.Contains(tray.noticeTexts(), "STKR updated to 0.3.0.") })
	tray.Quit()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestOpenDataFolder(t *testing.T) {
	op := &opener{}
	a := NewApp(Options{Home: `C:\data\stkr`, Open: op.open, Tray: newFakeTray()})
	if err := a.OpenDataFolder(); err != nil || op.all() != `C:\data\stkr` {
		t.Fatalf("opened %q, err %v", op.all(), err)
	}
	op.fail = true
	if err := a.OpenDataFolder(); err == nil {
		t.Error("an open failure was swallowed")
	}
}
