package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/sulthonuladib/e7-refresh/internal/engine"
)

// overlay is a small, click-through readout of what a run has bought. It is
// the modern equivalent of the hint window in the upstream Python app.
type overlay struct {
	win   *mygo.Window
	stats engine.Stats
}

func newOverlay() *overlay {
	o := &overlay{stats: engine.Stats{Counts: map[string]int{}}}
	work := mygo.Screen.PrimaryDisplay().WorkArea
	const width, height, margin = 280, 124, 18
	x := work.X + work.Width - width - margin
	y := work.Y + work.Height - height - margin
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:       "E7 Refresh stats",
		Width:       width,
		Height:      height,
		X:           x,
		Y:           y,
		Frameless:   true,
		AlwaysOnTop: true,
		SkipTaskbar: true,
		Hidden:      true,
		Content:     ui.View(o.view),
	})
	win.SetIgnoreMouseEvents(true) // never steals clicks from the game
	o.win = win
	return o
}

func (o *overlay) set(s engine.Stats, enabled bool) {
	o.stats = s
	if o.win == nil {
		return
	}
	if enabled && s.Running {
		o.win.Show()
	} else {
		o.win.Hide()
	}
}

func (o *overlay) view(c *ui.Context) {
	accent := ui.RGB(110, 110, 118)
	if o.stats.Running {
		accent = ui.RGB(255, 214, 10)
	}
	ui.Box(c).Background(ui.RGB(20, 20, 23)).Radius(10).Padding(14).Gap(8).Fill().Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(accent)
			ui.Text(c, "E7 REFRESH").Bold()
		})
		ui.Text(c, fmt.Sprintf("%s  ·  %d refreshes  ·  %d skystones",
			o.stats.Mode, o.stats.RefreshCount, o.stats.Spent)).FontSize(12)
		ui.Row(c).Gap(14).Children(func() {
			itemLine(c, ui.RGB(94, 165, 255), "Cov", o.stats.Counts["cov"])
			itemLine(c, ui.RGB(255, 106, 106), "Mys", o.stats.Counts["mys"])
			itemLine(c, ui.RGB(196, 160, 110), "Fb", o.stats.Counts["fb"])
		})
	})
}

func itemLine(c *ui.Context, color ui.Color, name string, count int) {
	ui.Row(c).Gap(6).Children(func() {
		ui.Box(c).Width(7).Height(7).Radius(4).Background(color)
		ui.Text(c, fmt.Sprintf("%s %d", name, count)).FontSize(12)
	})
}

// itemColor keeps the settings window and the overlay in sync.
func itemColor(id string) ui.Color {
	switch id {
	case "cov":
		return ui.RGB(94, 165, 255)
	case "mys":
		return ui.RGB(255, 106, 106)
	default:
		return ui.RGB(196, 160, 110)
	}
}
