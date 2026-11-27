package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/sulthonuladib/e7-refresh/internal/config"
	"github.com/sulthonuladib/e7-refresh/internal/daemon"
	"github.com/sulthonuladib/e7-refresh/internal/desktop"
	"github.com/sulthonuladib/e7-refresh/internal/engine"
	"github.com/sulthonuladib/e7-refresh/internal/platform"
)

// trayRefs are the menu items the status poller keeps up to date.
type trayRefs struct {
	tray      *mygo.Tray
	status    *mygo.MenuItem
	start     *mygo.MenuItem
	stop      *mygo.MenuItem
	modeMouse *mygo.MenuItem
	modeAdb   *mygo.MenuItem
	items     map[string]*mygo.MenuItem
	budgets   map[int]*mygo.MenuItem
	lastRun   bool
	mu        sync.Mutex
}

// trayHolder keeps the current tray pointer so the status poller can keep
// updating the right menu when the tray is turned off and on again.
type trayHolder struct {
	mu   sync.Mutex
	refs *trayRefs
}

func (h *trayHolder) set(refs *trayRefs) {
	h.mu.Lock()
	h.refs = refs
	h.mu.Unlock()
}

func (h *trayHolder) get() *trayRefs {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.refs
}

func (h *trayHolder) update(stats engine.Stats) {
	mygo.RunOnMain(func() { updateTray(h.get(), stats) })
}

func runApp(client *daemon.Client, server *daemon.Server, showWindow bool) {
	// The app lives next to a dark desktop; keep its own surfaces dark too.
	mygo.Theme.SetSource(mygo.ThemeDark)
	info := desktop.Detect()
	omarchy := info.Desktop == "omarchy"
	dash := newDashboard(client)
	dash.omarchy = omarchy
	// Omarchy's shell is the control surface; a StatusNotifier tray is not
	// shown there. Turn the setting off for good so the window and the CLI
	// report the same configuration on the next launch.
	if omarchy && dash.cfg.TrayEnabled {
		cfg := dash.cfg
		cfg.TrayEnabled = false
		dash.setConfig(cfg)
		_ = config.Save(cfg)
	}
	if omarchy {
		go ensureOmarchyWidget()
	}
	holder := &trayHolder{}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:       "E7 Secret Shop Refresh",
			Width:       520,
			Height:      700,
			Hidden:      true,
			SkipTaskbar: false,
			Content:     ui.View(dash.view),
		})
		dash.win = win
		// Closing the window hides it: the service keeps running so the
		// bar and the background service still work while it is closed.
		win.OnClose(func(e *mygo.CloseEvent) {
			e.PreventDefault()
			win.Hide()
		})
		if server != nil {
			server.SetOpenFunc(func() { dash.show() })
			server.SetQuitFunc(func() { mygo.App.Quit() })
			server.SetTrayFunc(func(enabled bool) {
				if omarchy {
					return // no tray on Omarchy
				}
				mygo.RunOnMain(func() {
					cfg := dash.currentConfig()
					cfg.TrayEnabled = enabled
					dash.setConfig(cfg)
					current := holder.get()
					if enabled {
						if current == nil || current.tray == nil {
							holder.set(buildTray(client, dash))
						}
					} else if current != nil && current.tray != nil {
						current.tray.Destroy()
						current.tray = nil
						dash.show() // keep a way to turn it back on
					}
				})
			})
		}
		if !omarchy && dash.cfg.TrayEnabled {
			holder.set(buildTray(client, dash))
		}
		var overlay *overlay
		if !omarchy {
			// Omarchy shows these numbers in its bar widget instead; other
			// desktops keep the small always-on-top readout.
			overlay = newOverlay()
		}
		go pollState(client, dash, holder, overlay)
		if !omarchy || showWindow {
			dash.show()
		} else {
			go summonPanelOrShow(win)
		}
		// A second launch (launcher, menu or `e7-refresh`) activates the
		// running instance. MyGo passes the launcher's activation token, so
		// this also works on Wayland where an app cannot raise itself.
		mygo.App.OnSecondInstance(func(args []string, _ string) {
			wantsSettings := showWindow
			for _, arg := range args {
				if strings.EqualFold(arg, "settings") {
					wantsSettings = true
				}
			}
			mygo.RunOnMain(func() {
				if omarchy && !wantsSettings {
					go func() {
						if !summonOmarchyPanel() {
							mygo.RunOnMain(dash.show)
						}
					}()
					return
				}
				dash.show()
			})
		})
	})
	if err := mygo.App.Run(); err != nil {
		fail(err)
	}
}

// ensureOmarchyWidget installs the QML bar widget on first launch so the
// shell, not a tray icon, is the way in. It never touches /usr/share.
func ensureOmarchyWidget() {
	if err := ensureBarPlugin(); err != nil {
		fmt.Fprintln(os.Stderr, "e7-refresh: Omarchy bar widget:", err)
	}
}

// summonOmarchyPanel asks the running shell to show the plugin panel. The
// shell hot-reloads plugins and shell.json, so this also works right after
// the first install; a few retries cover the reload.
func summonOmarchyPanel() bool {
	for attempt := 0; attempt < 20; attempt++ {
		out, err := platform.Run(4*time.Second, "omarchy-shell", "shell", "summon", "e7-refresh", "{}")
		if err == nil && strings.TrimSpace(string(out)) == "ok" {
			return true
		}
		if attempt == 2 {
			// A first install adds the plugin while the shell is running.
			// Rescanning makes the bar pick up the widget without a restart.
			_, _ = platform.Run(5*time.Second, "omarchy-shell", "shell", "rescanPlugins")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// summonPanelOrShow keeps the app usable when the shell is not running or
// the plugin could not be enabled: the settings window becomes the fallback.
func summonPanelOrShow(win *mygo.Window) {
	time.Sleep(400 * time.Millisecond)
	if summonOmarchyPanel() {
		return
	}
	mygo.RunOnMain(func() {
		win.Show()
		win.Focus()
		go floatSettingsWindow()
	})
}

func buildTray(client *daemon.Client, dash *dashboard) *trayRefs {
	info := desktop.Detect()
	refs := &trayRefs{items: map[string]*mygo.MenuItem{}, budgets: map[int]*mygo.MenuItem{}}
	refs.status = &mygo.MenuItem{ID: "status", Label: "Idle", Disabled: true}
	refs.start = &mygo.MenuItem{ID: "start", Label: "Start refresh",
		Click: func(*mygo.MenuItem, *mygo.Window) { dash.start() }}
	refs.stop = &mygo.MenuItem{ID: "stop", Label: "Stop refresh",
		Click: func(*mygo.MenuItem, *mygo.Window) { go call(client, daemon.Request{Cmd: "stop"}) }}
	refs.stop.Disabled = true

	modeItems := []*mygo.MenuItem{
		{ID: "mode-mouse", Label: "This desktop", Type: mygo.MenuItemRadio,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) { dash.setMode("mouse") }},
		{ID: "mode-adb", Label: "ADB", Type: mygo.MenuItemRadio,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) { dash.setMode("adb") }},
	}
	refs.modeMouse, refs.modeAdb = modeItems[0], modeItems[1]

	var itemMenu []*mygo.MenuItem
	for _, item := range config.Items {
		id := item.ID
		refs.items[id] = &mygo.MenuItem{ID: "item-" + id, Label: item.Name, Type: mygo.MenuItemCheckbox,
			Checked: dash.cfg.Items[id],
			Click: func(menuItem *mygo.MenuItem, _ *mygo.Window) {
				dash.setItem(id, menuItem.IsChecked())
			}}
		itemMenu = append(itemMenu, refs.items[id])
	}
	var budgetMenu []*mygo.MenuItem
	for _, value := range []int{0, 100, 500, 1000, 3000, 10000} {
		budget := value
		label := "Unlimited"
		if budget > 0 {
			label = fmt.Sprintf("%d skystones", budget)
		}
		refs.budgets[budget] = &mygo.MenuItem{ID: fmt.Sprintf("budget-%d", budget),
			Label: label, Type: mygo.MenuItemRadio,
			Click: func(*mygo.MenuItem, *mygo.Window) { dash.setBudget(budget) }}
		budgetMenu = append(budgetMenu, refs.budgets[budget])
	}

	barItem := &mygo.MenuItem{ID: "install-bar", Label: "Install Omarchy bar widget",
		Hidden: info.Desktop != "omarchy" || barWidgetInstalled(),
		Click: func(*mygo.MenuItem, *mygo.Window) {
			go func() {
				if err := installBarE(); err != nil {
					notify("Could not install the bar widget: " + err.Error())
					return
				}
				notify("Bar widget installed. Restart the shell if it does not appear.")
			}()
		}}

	serviceLabel := "Install background service"
	if _, err := os.Stat(serviceUnitPath()); err == nil {
		serviceLabel = "Remove background service"
	}
	serviceItem := &mygo.MenuItem{ID: "service", Label: serviceLabel,
		Click: func(*mygo.MenuItem, *mygo.Window) {
			go func() {
				if _, err := os.Stat(serviceUnitPath()); err == nil {
					if err := removeServiceE(); err != nil {
						notify("Could not remove the service: " + err.Error())
						return
					}
					notify("Background service removed.")
					return
				}
				if err := installServiceE(); err != nil {
					notify("Could not install the service: " + err.Error())
					return
				}
				notify("Background service installed and enabled.")
			}()
		}}

	trayItem := &mygo.MenuItem{ID: "tray-toggle", Label: "Show tray icon",
		Type: mygo.MenuItemCheckbox, Checked: dash.cfg.TrayEnabled,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			go call(client, daemon.Request{Cmd: "tray",
				Value: strconv.FormatBool(item.IsChecked())})
			if !item.IsChecked() {
				dash.show()
			}
		}}

	menu := mygo.NewMenu([]*mygo.MenuItem{
		refs.status,
		mygo.Separator(),
		refs.start,
		refs.stop,
		{ID: "open", Label: "Open settings", Accelerator: "CmdOrCtrl+Shift+O",
			Click: func(*mygo.MenuItem, *mygo.Window) { dash.show() }},
		mygo.Separator(),
		{Label: "Mode", Submenu: modeItems},
		{Label: "Items to buy", Submenu: itemMenu},
		{Label: "Budget", Submenu: budgetMenu},
		mygo.Separator(),
		{ID: "history", Label: "Open refresh history",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				go mygo.Shell.OpenPath(config.HistoryDir())
			}},
		barItem,
		serviceItem,
		trayItem,
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})

	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "e7-refresh",
		Icon:           trayIdle,
		IconIsTemplate: true,
		ToolTip:        "E7 Secret Shop Refresh",
		Menu:           menu,
	})
	if err != nil {
		// Say what is wrong, then keep the settings window available so the
		// app is still usable without a notification area.
		fmt.Fprintln(os.Stderr, "e7-refresh: tray:", err)
		go notify("Tray unavailable: " + err.Error() + ". The settings window is open instead.")
		mygo.RunOnMain(dash.show)
	} else {
		refs.tray = tray
	}
	refs.modeMouse.SetChecked(dash.cfg.Mode == "mouse")
	refs.modeAdb.SetChecked(dash.cfg.Mode == "adb")
	for id, item := range refs.items {
		item.SetChecked(dash.cfg.Items[id])
	}
	for budget, item := range refs.budgets {
		item.SetChecked(budget == dash.cfg.Budget)
	}
	updateTray(refs, engine.Stats{Counts: map[string]int{}})
	return refs
}

func updateTray(refs *trayRefs, s engine.Stats) {
	if refs == nil {
		return
	}
	refs.mu.Lock()
	defer refs.mu.Unlock()
	label := s.LastResult
	if s.Running {
		label = fmt.Sprintf("Refreshing · %d refreshes", s.RefreshCount)
	}
	if label == "" {
		label = "Idle"
	}
	refs.status.SetLabel(label)
	refs.start.SetEnabled(!s.Running)
	refs.stop.SetEnabled(s.Running)
	if refs.tray != nil {
		if s.Running {
			_ = refs.tray.SetIcon(trayActive, false)
			refs.tray.SetToolTip(label)
		} else {
			_ = refs.tray.SetIcon(trayIdle, true)
			refs.tray.SetToolTip("E7 Secret Shop Refresh")
		}
		if refs.lastRun && !s.Running {
			go notify("Refresh finished: " + s.LastResult)
		}
	}
	refs.lastRun = s.Running
}

func pollState(client *daemon.Client, dash *dashboard, holder *trayHolder, overlay *overlay) {
	if response, err := client.Call(daemon.Request{Cmd: "get-config"}); err == nil && response.Config != nil {
		cfg := *response.Config
		mygo.RunOnMain(func() { dash.setConfig(cfg) })
	}
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		response, err := client.Call(daemon.Request{Cmd: "status"})
		if err != nil || response.Status == nil {
			continue
		}
		s := *response.Status
		mygo.RunOnMain(func() {
			dash.setStatus(s)
			if overlay != nil {
				overlay.set(s, dash.cfg.Hint)
			}
		})
		holder.update(s)
	}
}

func call(client *daemon.Client, req daemon.Request) {
	response, err := client.Call(req)
	if err != nil {
		go notify(err.Error())
		return
	}
	if response.Error != "" {
		go notify(response.Error)
	}
}

func notify(body string) {
	_ = mygo.NewNotification(mygo.NotificationOptions{
		Title: "E7 Refresh",
		Body:  body,
	}).Show()
}
