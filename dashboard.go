package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/sulthonuladib/e7-refresh/internal/config"
	"github.com/sulthonuladib/e7-refresh/internal/daemon"
	"github.com/sulthonuladib/e7-refresh/internal/engine"
)

// dashboard is the optional settings window. It is a thin controller: every
// action goes through the local service, so the bar widget sees the same
// state as the tray and the CLI.
type dashboard struct {
	client          *daemon.Client
	win             *mygo.Window
	cfg             config.Config
	status          engine.Stats
	title           string
	mode            string
	buyCov          bool
	buyMys          bool
	buyFb           bool
	budget          float64
	mouseSleep      float64
	screenshotSleep float64
	tapSleep        float64
	adbAddress      string
	message         string
}

func newDashboard(client *daemon.Client) *dashboard {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	cfg.Normalize()
	d := &dashboard{client: client, cfg: cfg, status: engine.Stats{Counts: map[string]int{}}}
	d.setConfig(cfg)
	return d
}

func (d *dashboard) setConfig(cfg config.Config) {
	cfg.Normalize()
	d.cfg = cfg
	d.title = cfg.WindowTitle
	d.mode = cfg.Mode
	d.buyCov = cfg.Items["cov"]
	d.buyMys = cfg.Items["mys"]
	d.buyFb = cfg.Items["fb"]
	d.budget = float64(cfg.Budget)
	d.mouseSleep = cfg.MouseSleep
	d.screenshotSleep = cfg.ScreenshotSleep
	d.tapSleep = cfg.TapSleep
	d.adbAddress = cfg.ADBAddress
}

func (d *dashboard) setStatus(s engine.Stats) { d.status = s }

func (d *dashboard) show() {
	if d.win != nil {
		d.win.Show()
		d.win.Focus()
	}
}

func (d *dashboard) currentConfig() config.Config {
	cfg := d.cfg
	cfg.WindowTitle = d.title
	cfg.Mode = d.mode
	cfg.Items = map[string]bool{"cov": d.buyCov, "mys": d.buyMys, "fb": d.buyFb}
	cfg.Budget = int(d.budget)
	cfg.MouseSleep = d.mouseSleep
	cfg.ScreenshotSleep = d.screenshotSleep
	cfg.TapSleep = d.tapSleep
	cfg.ADBAddress = d.adbAddress
	cfg.Normalize()
	return cfg
}

func (d *dashboard) save() {
	cfg := d.currentConfig()
	d.setConfig(cfg)
	go func() {
		_, err := d.client.Call(daemon.Request{Cmd: "set-config", Config: &cfg})
		message := "Settings saved"
		if err != nil {
			message = err.Error()
		}
		mygo.RunOnMain(func() { d.message = message })
	}()
}

func (d *dashboard) start() {
	cfg := d.currentConfig()
	go func() {
		_, err := d.client.Call(daemon.Request{Cmd: "start", Config: &cfg})
		message := "Refresh started"
		if err != nil {
			message = err.Error()
		}
		mygo.RunOnMain(func() { d.message = message })
	}()
}

func (d *dashboard) stop() {
	go call(d.client, daemon.Request{Cmd: "stop"})
}

func (d *dashboard) setMode(mode string) {
	d.mode = mode
	d.save()
}

func (d *dashboard) setBudget(budget int) {
	d.budget = float64(budget)
	d.save()
}

func (d *dashboard) setItem(id string, checked bool) {
	switch id {
	case "cov":
		d.buyCov = checked
	case "mys":
		d.buyMys = checked
	case "fb":
		d.buyFb = checked
	}
	d.save()
}

func (d *dashboard) stateLine() string {
	s := d.status
	if s.Running {
		return fmt.Sprintf("Refreshing (%s) · %d refreshes · %d skystones",
			s.Mode, s.RefreshCount, s.Spent)
	}
	if s.LastResult != "" {
		return "Last run: " + s.LastResult
	}
	return "Idle"
}

func (d *dashboard) view(c *ui.Context) {
	ui.Column(c).Padding(20).Gap(8).Children(func() {
		ui.Text(c, "E7 Secret Shop Refresh").FontSize(19).Bold()
		ui.Text(c, d.stateLine())
		ui.Row(c).Gap(8).Children(func() {
			if d.status.Running {
				if ui.Button(c, "Stop").Clicked() {
					d.stop()
				}
			} else if ui.PrimaryButton(c, "Start refresh").Clicked() {
				d.start()
			}
			if ui.Button(c, "Hide").Clicked() && d.win != nil {
				d.win.Hide()
			}
		})
		ui.Text(c, d.message)

		ui.Divider(c)
		ui.Text(c, "Emulator window title").Bold()
		ui.TextInput(c, &d.title)

		ui.Text(c, "Mode: "+d.mode)
		if ui.Button(c, "Toggle mouse / ADB").Clicked() {
			if d.mode == "adb" {
				d.setMode("mouse")
			} else {
				d.setMode("adb")
			}
		}

		ui.Text(c, "Items to buy").Bold()
		ui.Row(c).Gap(12).Children(func() {
			ui.Checkbox(c, &d.buyCov, "Covenant")
			ui.Checkbox(c, &d.buyMys, "Mystic")
			ui.Checkbox(c, &d.buyFb, "Friendship")
		})

		ui.Text(c, "Skystone budget (0 = until stopped)")
		ui.NumberInput(c, &d.budget, 0, 100000, 100)
		ui.Text(c, "Mouse speed (seconds)")
		ui.NumberInput(c, &d.mouseSleep, 0.01, 10, 0.1)
		ui.Text(c, "Screenshot speed (seconds)")
		ui.NumberInput(c, &d.screenshotSleep, 0.01, 10, 0.1)
		ui.Text(c, "ADB tap speed (seconds)")
		ui.NumberInput(c, &d.tapSleep, 0.01, 10, 0.1)
		ui.Text(c, "ADB address").Bold()
		ui.TextInput(c, &d.adbAddress)

		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, "Save settings").Clicked() {
				d.save()
			}
			if ui.Button(c, "Open history").Clicked() {
				go mygo.Shell.OpenPath(config.HistoryDir())
			}
		})
	})
}
