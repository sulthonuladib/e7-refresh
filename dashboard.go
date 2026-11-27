package main

import (
	"fmt"
	"os"

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
	windowWidth     float64
	windowHeight    float64
	templateWidth   float64
	adbAddress      string
	trayEnabled     bool
	omarchy         bool
	trayApplied     bool
	hint            bool
	matchThreshold  float64
	templateDir     string
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
	d.windowWidth = float64(cfg.WindowWidth)
	d.windowHeight = float64(cfg.WindowHeight)
	d.templateWidth = float64(cfg.TemplateWidth)
	d.adbAddress = cfg.ADBAddress
	d.trayEnabled = cfg.TrayEnabled
	d.trayApplied = cfg.TrayEnabled
	d.hint = cfg.Hint
	d.matchThreshold = cfg.MatchThreshold
	d.templateDir = cfg.TemplateDir
}

func (d *dashboard) setStatus(s engine.Stats) { d.status = s }

func (d *dashboard) show() {
	if d.win == nil {
		return
	}
	d.win.Show()
	d.win.Focus()
	// On a tiling compositor the window would otherwise fill a tile at a
	// size the two-column form was not laid out for. Float and centre it;
	// the helper waits for the window manager to map the window.
	go floatSettingsWindow()
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
	cfg.WindowWidth = int(d.windowWidth)
	cfg.WindowHeight = int(d.windowHeight)
	cfg.TemplateWidth = int(d.templateWidth)
	cfg.ADBAddress = d.adbAddress
	cfg.TrayEnabled = d.trayEnabled
	cfg.Hint = d.hint
	cfg.MatchThreshold = d.matchThreshold
	cfg.TemplateDir = d.templateDir
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

func (d *dashboard) applyTray() {
	value := "off"
	if d.trayEnabled {
		value = "on"
	}
	d.trayApplied = d.trayEnabled
	d.save()
	go call(d.client, daemon.Request{Cmd: "tray", Value: value})
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
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(18).Gap(14).Fill().Children(func() {
			ui.Row(c).Gap(10).Children(func() {
				ui.Text(c, "E7 Secret Shop Refresh").FontSize(18).Bold()
				if d.status.Running {
					ui.Box(c).Background(ui.RGB(255, 214, 10)).Radius(4).Padding(4, 8).Children(func() {
						ui.Text(c, "RUNNING").FontSize(11).Bold()
					})
				}
			})
			ui.Text(c, d.stateLine()).FontSize(13)
			ui.Row(c).Gap(10).Children(func() {
				if d.status.Running {
					if ui.Button(c, "Stop refresh").Clicked() {
						d.stop()
					}
				} else if ui.PrimaryButton(c, "Start refresh").Clicked() {
					d.start()
				}
				if ui.Button(c, "Hide window").Clicked() && d.win != nil {
					d.win.Hide()
				}
			})
			if d.message != "" {
				ui.Text(c, d.message).FontSize(12)
			}
			ui.Divider(c)

			ui.Row(c).Gap(24).Children(func() {
				ui.Column(c).Gap(10).Fill().Children(func() {
					section(c, "Refresh")
					field(c, "Mode", func() {
						ui.Select(c, &d.mode, []string{"mouse", "adb"})
						if d.mode != d.cfg.Mode {
							d.setMode(d.mode)
						}
					})
					section(c, "Items to buy")
					itemCheckbox(c, "cov", "Covenant", &d.buyCov)
					itemCheckbox(c, "mys", "Mystic", &d.buyMys)
					itemCheckbox(c, "fb", "Friendship", &d.buyFb)
					field(c, "Skystone budget (0 = until stopped)", func() {
						ui.NumberInput(c, &d.budget, 0, 100000, 100)
					})
					if !d.omarchy {
						ui.Checkbox(c, &d.hint, "Show bought-items overlay")
					}
					field(c, "Match sensitivity (lower finds more)", func() {
						ui.NumberInput(c, &d.matchThreshold, 0.4, 0.95, 0.01)
					})
				})

				ui.Column(c).Gap(10).Fill().Children(func() {
					section(c, "Setup")
					field(c, "Emulator window title", func() { ui.TextInput(c, &d.title) })
					field(c, "Mouse speed (seconds)", func() {
						ui.NumberInput(c, &d.mouseSleep, 0.01, 10, 0.1)
					})
					field(c, "Screenshot speed (seconds)", func() {
						ui.NumberInput(c, &d.screenshotSleep, 0.01, 10, 0.1)
					})
					field(c, "ADB tap speed (seconds)", func() {
						ui.NumberInput(c, &d.tapSleep, 0.01, 10, 0.1)
					})
					field(c, "ADB address", func() { ui.TextInput(c, &d.adbAddress) })
					field(c, "Game window size (mouse mode)", func() {
						ui.Row(c).Gap(6).Children(func() {
							ui.NumberInput(c, &d.windowWidth, 320, 3840, 10)
							ui.Text(c, "×").FontSize(12)
							ui.NumberInput(c, &d.windowHeight, 240, 2160, 10)
						})
					})
					field(c, "Template crop width (advanced)", func() {
						ui.NumberInput(c, &d.templateWidth, 200, 4096, 1)
					})
					field(c, "Custom template folder (optional)", func() {
						ui.TextInput(c, &d.templateDir)
					})
					if d.omarchy {
						section(c, "Omarchy")
						ui.Text(c, barWidgetStatus()).FontSize(12)
						if !barWidgetInstalled() {
							if ui.Button(c, "Install bar widget").Clicked() {
								go func() {
									err := installBarE()
									message := "Bar widget installed"
									if err != nil {
										message = err.Error()
									}
									mygo.RunOnMain(func() { d.message = message })
								}()
							}
						}
					} else {
						ui.Checkbox(c, &d.trayEnabled, "Show tray icon")
						if d.trayEnabled != d.trayApplied {
							d.applyTray()
						}
					}
					serviceButton(c, d)
				})
			})

			ui.Row(c).Gap(10).Children(func() {
				if ui.PrimaryButton(c, "Save settings").Clicked() {
					d.save()
				}
				if ui.Button(c, "Open history").Clicked() {
					go mygo.Shell.OpenPath(config.HistoryDir())
				}
			})
		})
	})
}

func section(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(15).Bold()
}

func field(c *ui.Context, label string, control func()) {
	ui.Text(c, label).FontSize(12)
	control()
}

func itemCheckbox(c *ui.Context, id, label string, checked *bool) {
	ui.Row(c).Gap(8).Children(func() {
		ui.Box(c).Width(9).Height(9).Radius(5).Background(itemColor(id))
		ui.Checkbox(c, checked, label)
	})
}

func barWidgetStatus() string {
	if barWidgetInstalled() {
		return "The Omarchy bar widget is installed. Click it for this panel; no tray icon is used."
	}
	return "The Omarchy bar widget is not installed yet."
}

func serviceButton(c *ui.Context, d *dashboard) {
	label := "Install background service"
	if _, err := os.Stat(serviceUnitPath()); err == nil {
		label = "Remove background service"
	}
	if ui.Button(c, label).Clicked() {
		go func() {
			var err error
			if _, statErr := os.Stat(serviceUnitPath()); statErr == nil {
				err = removeServiceE()
			} else {
				err = installServiceE()
			}
			message := "Background service updated"
			if err != nil {
				message = err.Error()
			}
			mygo.RunOnMain(func() { d.message = message })
		}()
	}
}
