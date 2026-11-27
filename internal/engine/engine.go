// Package engine runs the two refresh macros. It owns no GUI and no MyGo
// types, so the same code runs in the tray app and in the headless service.
package engine

import (
	"context"
	"encoding/csv"
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sulthonuladib/e7-refresh/internal/adb"
	"github.com/sulthonuladib/e7-refresh/internal/assets"
	"github.com/sulthonuladib/e7-refresh/internal/config"
	"github.com/sulthonuladib/e7-refresh/internal/platform"
	"github.com/sulthonuladib/e7-refresh/internal/vision"
)

// Stats is the state shared with the tray, the bar widget and the CLI.
type Stats struct {
	Running      bool            `json:"running"`
	Mode         string          `json:"mode"`
	StartedAt    time.Time       `json:"startedAt,omitempty"`
	FinishedAt   time.Time       `json:"finishedAt,omitempty"`
	RefreshCount int             `json:"refreshCount"`
	Counts       map[string]int  `json:"counts"`
	Items        map[string]bool `json:"items"`
	Spent        int             `json:"spent"`
	Gold         int             `json:"gold"`
	Window       string          `json:"window,omitempty"`
	Budget       int             `json:"budget"`
	LastError    string          `json:"lastError,omitempty"`
	LastResult   string          `json:"lastResult,omitempty"`
}

// Engine starts and stops refresh runs and publishes their state.
type Engine struct {
	mu            sync.RWMutex
	stopReason    string
	budgetReached bool
	stats         Stats
	cfg           config.Config
	cancel        context.CancelFunc
	done          chan struct{}
	onChange      func(Stats)
}

// New creates an engine. onChange may be nil; it is called off the UI thread.
func New(onChange func(Stats)) *Engine {
	return &Engine{
		stats:    Stats{Counts: map[string]int{}},
		cfg:      config.Default(),
		onChange: onChange,
	}
}

// Status returns a copy of the current state.
func (e *Engine) Status() Stats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := e.stats
	out.Counts = make(map[string]int, len(e.stats.Counts))
	for k, v := range e.stats.Counts {
		out.Counts[k] = v
	}
	out.Items = make(map[string]bool, len(e.stats.Items))
	for k, v := range e.stats.Items {
		out.Items[k] = v
	}
	return out
}

// Config returns the settings the engine is using.
func (e *Engine) Config() config.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// SetConfig replaces the settings used by the next run.
func (e *Engine) SetConfig(c config.Config) {
	c.Normalize()
	e.mu.Lock()
	e.cfg = c
	e.mu.Unlock()
	e.publish()
}

func (e *Engine) publish() {
	if e.onChange != nil {
		e.onChange(e.Status())
	}
}

// Start validates settings and launches a refresh run in the background.
func (e *Engine) Start(c config.Config) error {
	c.Normalize()
	e.mu.Lock()
	if e.stats.Running {
		e.mu.Unlock()
		return fmt.Errorf("refresh is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	e.cfg = c
	items := map[string]bool{}
	for id, enabled := range c.Items {
		items[id] = enabled
	}
	e.stats = Stats{
		Running: true, Mode: c.Mode, StartedAt: time.Now(),
		Counts: map[string]int{}, Items: items,
		Window: c.WindowTitle, Budget: c.Budget,
	}
	e.mu.Unlock()
	e.publish()

	go func() {
		defer close(e.done)
		defer cancel()
		var err error
		if c.Mode == "adb" {
			err = e.runADB(ctx, c)
		} else {
			err = e.runMouse(ctx, c)
		}
		e.finish(err)
	}()
	return nil
}

// Stop asks the running macro to finish after its current action.
func (e *Engine) Stop() error { return e.StopFrom("a stop request") }

// StopFrom stops the run and records what asked for it, so the summary reads
// "stopped by F10" or "stopped from the panel" instead of looking like the
// run gave up by itself.
func (e *Engine) StopFrom(reason string) error {
	e.mu.Lock()
	if !e.stats.Running {
		e.mu.Unlock()
		return nil
	}
	e.stopReason = reason
	cancel := e.cancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// Wait blocks until the current run finishes.
func (e *Engine) Wait() {
	e.mu.RLock()
	done := e.done
	e.mu.RUnlock()
	if done != nil {
		<-done
	}
}

func (e *Engine) finish(err error) {
	e.mu.Lock()
	e.stats.Running = false
	e.stats.FinishedAt = time.Now()
	e.stats.Spent = e.stats.RefreshCount * 3
	e.stats.Gold = 0
	for _, item := range config.Items {
		e.stats.Gold += item.Price * e.stats.Counts[item.ID]
	}
	if err != nil && err != context.Canceled {
		e.stats.LastError = err.Error()
	} else {
		e.stats.LastError = ""
	}
	e.stats.LastResult = fmt.Sprintf("%d refreshes, %d skystones, %s gold",
		e.stats.RefreshCount, e.stats.Spent, formatNumber(e.stats.Gold))
	if e.stopReason != "" {
		e.stats.LastResult += " · stopped by " + e.stopReason
	}
	if e.budgetReached {
		e.stats.LastResult += " · budget reached"
	}
	e.stopReason = ""
	e.budgetReached = false
	e.cancel = nil
	e.mu.Unlock()
	e.publish()
}

func formatNumber(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func enabled(c config.Config) []config.Item {
	var items []config.Item
	for _, item := range config.Items {
		if c.Items[item.ID] {
			items = append(items, item)
		}
	}
	return items
}

func loadTemplates(kind string, c config.Config, items []config.Item) (map[string]*vision.Template, error) {
	templates := map[string]*vision.Template{}
	for _, item := range items {
		var path string
		var err error
		if c.TemplateDir != "" {
			path = filepath.Join(c.TemplateDir, item.File)
			if _, err = os.Stat(path); err != nil {
				return nil, fmt.Errorf("custom template %s: %w", path, err)
			}
		} else {
			path, err = assets.Pattern(kind, item.File)
			if err != nil {
				return nil, err
			}
		}
		t, err := vision.LoadTemplate(item.ID, path)
		if err != nil {
			return nil, err
		}
		templates[item.ID] = t
	}
	if len(templates) == 0 {
		return nil, fmt.Errorf("no items selected; enable covenant or mystic bookmarks")
	}
	return templates, nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func crop(img image.Image, r platform.Rect) image.Image {
	box := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
	out := image.NewRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
	draw.Draw(out, out.Bounds(), img, box.Min, draw.Src)
	return out
}

func (e *Engine) increment(id string) {
	e.mu.Lock()
	e.stats.Counts[id]++
	e.mu.Unlock()
}

func (e *Engine) incrementRefresh() {
	e.mu.Lock()
	e.stats.RefreshCount++
	e.mu.Unlock()
}

func (e *Engine) overBudget(c config.Config) bool {
	return c.Budget > 0 && e.Status().RefreshCount >= c.Budget/3
}

func (e *Engine) runMouse(ctx context.Context, c config.Config) error {
	if strings.TrimSpace(c.WindowTitle) == "" {
		return fmt.Errorf("set the emulator window title before starting mouse refresh")
	}
	desktop := platform.New()
	if closer, ok := desktop.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	if missing := platform.Missing(desktop.Requirements()); len(missing) > 0 {
		return fmt.Errorf("%s", platform.FormatRequirements(missing))
	}
	window, err := desktop.FindWindow(c.WindowTitle)
	if err != nil {
		return err
	}
	// On Hyprland an XWayland window is captured and clicked in the background:
	// nothing is focused, nothing is moved, and the macro can run while the user
	// works on another workspace. When that is not available the old path still
	// moves the real pointer, so on Wayland it is refused rather than silently
	// taking over the user's cursor.
	background := desktop.Background()
	if !background && desktop.Kind() == "wayland" {
		return fmt.Errorf("window %q cannot be driven in the background: it must run through Xwayland (Proton/Steam games do) so its pixels and clicks stay inside it", c.WindowTitle)
	}
	if !background {
		if err := desktop.Activate(window); err != nil {
			return fmt.Errorf("activate window: %w", err)
		}
	}
	if !c.AllowMove || background {
		window.Rect = platform.Rect{X: 0, Y: 0, W: c.WindowWidth, H: c.WindowHeight}
	}
	if err := desktop.MoveResize(window, window.Rect); err != nil {
		return fmt.Errorf("move window: %w", err)
	}
	// Hyprland re-centres a floating window while resizing it, so read the
	// rectangle back instead of assuming the request was honoured. Every
	// click below is placed relative to this rectangle.
	if rect, err := desktop.Geometry(window); err == nil && rect.Valid() {
		window.Rect = rect
	}
	templates, err := loadTemplates("assets", c, enabled(c))
	if err != nil {
		return err
	}
	if !sleep(ctx, duration(c.MouseSleep, 0.01)) {
		return ctx.Err()
	}
	if err := clickShop(ctx, desktop, window.Rect, duration(c.MouseSleep, 0.01)); err != nil {
		return err
	}
	if !sleep(ctx, time.Second) {
		return ctx.Err()
	}
	sliding := time.Duration(math.Max(0.7+c.ScreenshotSleep, 1) * float64(time.Second))
	defer e.writeHistory("mouse", c)

	for ctx.Err() == nil {
		if err := desktop.MoveResize(window, window.Rect); err != nil {
			return err
		}
		if rect, err := desktop.Geometry(window); err == nil && rect.Valid() {
			window.Rect = rect
		}
		if !sleep(ctx, sliding) {
			break
		}
		screen, scale, err := grabWindow(desktop, window.Rect, c.TemplateWidth)
		if err != nil {
			return err
		}
		bought := map[string]bool{}
		for id, template := range templates {
			_, y, _, ok := screen.MatchBest(template, c.MatchThreshold)
			if !ok {
				continue
			}
			if err := buyMouse(ctx, desktop, window.Rect, int(math.Round(float64(y)*scale)), c); err != nil {
				return err
			}
			e.increment(id)
			bought[id] = true
		}
		if ctx.Err() != nil {
			break
		}
		if err := scrollShop(desktop, window.Rect); err != nil {
			return err
		}
		if !sleep(ctx, duration(c.ScreenshotSleep, 0.3)) {
			break
		}
		screen, scale, err = grabWindow(desktop, window.Rect, c.TemplateWidth)
		if err != nil {
			return err
		}
		for id, template := range templates {
			if bought[id] {
				continue
			}
			_, y, _, ok := screen.MatchBest(template, c.MatchThreshold)
			if !ok {
				continue
			}
			if err := buyMouse(ctx, desktop, window.Rect, int(math.Round(float64(y)*scale)), c); err != nil {
				return err
			}
			e.increment(id)
		}
		if e.overBudget(c) {
			e.mu.Lock()
			e.budgetReached = true
			e.mu.Unlock()
			break
		}
		if err := refreshMouse(ctx, desktop, window.Rect, c); err != nil {
			return err
		}
		e.incrementRefresh()
		if !sleep(ctx, duration(c.MouseSleep, 0.01)) {
			break
		}
	}
	return ctx.Err()
}

// grabWindow captures the window and scales it to the template width. It
// returns the factor that maps match positions back to screen pixels.
func grabWindow(d platform.Desktop, r platform.Rect, templateWidth int) (*vision.Screen, float64, error) {
	frame, err := d.Capture()
	if err != nil {
		return nil, 0, fmt.Errorf("screenshot: %w", err)
	}
	bounds := frame.Bounds()
	box := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
	if !box.In(bounds) {
		return nil, 0, fmt.Errorf("window %v is outside the captured screen %v; move it fully on screen or enable AllowMove", box, bounds)
	}
	if templateWidth <= 0 {
		templateWidth = vision.ReferenceWidth
	}
	return vision.Prepare(crop(frame, r), templateWidth),
		float64(r.W) / float64(templateWidth), nil
}

func clickShop(ctx context.Context, d platform.Desktop, r platform.Rect, pause time.Duration) error {
	clicks := []struct{ x, y float64 }{
		{0.05, 0.41}, {0.44, 0.26}, {0.05, 0.41},
	}
	for _, p := range clicks {
		if err := d.Click(r.X+int(float64(r.W)*p.x), r.Y+int(float64(r.H)*p.y)); err != nil {
			return err
		}
		if !sleep(ctx, pause) {
			return context.Canceled
		}
	}
	return nil
}

func buyMouse(ctx context.Context, d platform.Desktop, r platform.Rect, matchY int, c config.Config) error {
	x := r.X + int(float64(r.W)*0.90)
	y := r.Y + matchY + int(float64(r.H)*0.085)
	if err := d.DoubleClick(x, y, duration(c.MouseSleep, 0.01)); err != nil {
		return err
	}
	if !sleep(context.Background(), duration(c.MouseSleep, 0.01)) {
		return context.Canceled
	}
	if err := d.DoubleClick(
		r.X+int(float64(r.W)*0.55),
		r.Y+int(float64(r.H)*0.70),
		duration(c.MouseSleep, 0.01),
	); err != nil {
		return err
	}
	// Loading after a purchase needs the same pause the upstream app takes.
	sleep(ctx, duration(c.ScreenshotSleep, 0.01))
	return nil
}

func refreshMouse(ctx context.Context, d platform.Desktop, r platform.Rect, c config.Config) error {
	if err := d.DoubleClick(
		r.X+int(float64(r.W)*0.20),
		r.Y+int(float64(r.H)*0.90),
		duration(c.MouseSleep, 0.01),
	); err != nil {
		return err
	}
	if !sleep(context.Background(), duration(c.MouseSleep, 0.01)) {
		return context.Canceled
	}
	x := r.X + int(float64(r.W)*0.58)
	y := r.Y + int(float64(r.H)*0.65)
	if err := d.DoubleClick(x, y, duration(c.MouseSleep, 0.01)); err != nil {
		return err
	}
	sleep(ctx, duration(c.ScreenshotSleep, 0.01))
	return nil
}

func scrollShop(d platform.Desktop, r platform.Rect) error {
	x := r.X + int(float64(r.W)*0.58)
	y := r.Y + int(float64(r.H)*0.65)
	return d.Drag(x, y, x, y-int(float64(r.H)*0.277))
}

func (e *Engine) runADB(ctx context.Context, c config.Config) error {
	path, err := adb.Resolve(c.ADBPath)
	if err != nil {
		return err
	}
	device := &adb.ADB{Path: path, Device: c.ADBAddress}
	devices, err := adb.Devices(path)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		if err := device.Connect(c.ADBAddress); err != nil {
			return err
		}
	} else if c.ADBAddress != "" {
		found := false
		for _, id := range devices {
			if id == c.ADBAddress {
				found = true
				break
			}
		}
		if !found {
			if err := device.Connect(c.ADBAddress); err != nil {
				return err
			}
		} else {
			device.Device = c.ADBAddress
		}
	}
	w, h, err := device.CheckDimension()
	if err != nil {
		return err
	}
	if w != 1920 || h != 1080 {
		return fmt.Errorf("device resolution is %dx%d; set the emulator to 1920x1080", w, h)
	}
	templates, err := loadTemplates("adb-assets", c, enabled(c))
	if err != nil {
		return err
	}
	defer e.writeHistory("adb", c)

	sliding := duration(1.5, 0)
	if !sleep(ctx, sliding) {
		return ctx.Err()
	}
	for _, p := range []struct{ x, y float64 }{{0.0411, 0.3835}, {0.4406, 0.2462}, {0.0411, 0.3835}} {
		if err := device.Tap(float64(w)*p.x, float64(h)*p.y); err != nil {
			return err
		}
		if !sleep(ctx, 500*time.Millisecond) {
			return ctx.Err()
		}
	}
	for ctx.Err() == nil {
		if !sleep(ctx, sliding) {
			break
		}
		screen, err := adbScreen(device)
		if err != nil {
			return err
		}
		bought := map[string]bool{}
		for id, template := range templates {
			x, y, _, ok := screen.MatchBest(template, c.MatchThreshold)
			if !ok {
				continue
			}
			if err := buyADB(device, w, h, x, y, c); err != nil {
				return err
			}
			e.increment(id)
			bought[id] = true
		}
		if ctx.Err() != nil {
			break
		}
		xoff, yoff := offset(c.RandomOffset)
		if err := device.Swipe(0.625*float64(w)+xoff, 0.7481*float64(h)+yoff,
			0.625*float64(w)+xoff, 0.3629*float64(h)+yoff); err != nil {
			return err
		}
		if !sleep(ctx, time.Second) {
			break
		}
		screen, err = adbScreen(device)
		if err != nil {
			return err
		}
		for id, template := range templates {
			if bought[id] {
				continue
			}
			x, y, _, ok := screen.MatchBest(template, c.MatchThreshold)
			if !ok {
				continue
			}
			if err := buyADB(device, w, h, x, y, c); err != nil {
				return err
			}
			e.increment(id)
		}
		if e.overBudget(c) {
			e.mu.Lock()
			e.budgetReached = true
			e.mu.Unlock()
			break
		}
		xoff, yoff = offset(c.RandomOffset)
		if err := device.Tap(0.1698*float64(w)+xoff, 0.9138*float64(h)+yoff); err != nil {
			return err
		}
		if !sleep(ctx, duration(c.TapSleep, 0.3)) {
			break
		}
		xoff, yoff = offset(c.RandomOffset)
		if err := device.Tap(0.5828*float64(w)+xoff, 0.6411*float64(h)+yoff); err != nil {
			return err
		}
		e.incrementRefresh()
	}
	return ctx.Err()
}

// adbScreen normalises the frame to the ADB templates' reference width.
// runADB refuses any resolution but 1920x1080, so this is the native size.
func adbScreen(device *adb.ADB) (*vision.Screen, error) {
	img, err := device.Screenshot()
	if err != nil {
		return nil, err
	}
	return vision.Prepare(img, 1920), nil
}

func buyADB(device *adb.ADB, w, h, matchX, matchY int, c config.Config) error {
	xoff, yoff := offset(c.RandomOffset)
	if err := device.Tap(float64(matchX)+0.4718*float64(w)+xoff,
		float64(matchY)+0.1000*float64(h)+yoff); err != nil {
		return err
	}
	if !sleep(context.Background(), duration(c.TapSleep, 0.3)) {
		return context.Canceled
	}
	xoff, yoff = offset(c.RandomOffset)
	if err := device.Tap(0.5677*float64(w)+xoff, 0.7037*float64(h)+yoff); err != nil {
		return err
	}
	if !sleep(context.Background(), duration(c.TapSleep, 0.3)+time.Second) {
		return context.Canceled
	}
	return nil
}

func offset(random bool) (float64, float64) {
	if !random {
		return 0, 0
	}
	// Small jitter makes the clicks less mechanical without moving them out
	// of the button. The ranges match the upstream debug overlay.
	return float64((time.Now().UnixNano() % 151) - 75), float64((time.Now().UnixNano() % 51) - 25)
}

func duration(seconds, minimum float64) time.Duration {
	if seconds < minimum {
		seconds = minimum
	}
	return time.Duration(seconds * float64(time.Second))
}

// writeHistory appends one row to the CSV file, creating the header when
// needed. The file stays in the user's data directory, never beside the
// original game files.
func (e *Engine) writeHistory(mode string, c config.Config) {
	s := e.Status()
	dir := config.HistoryDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	items := enabled(c)
	name := "ADB_History.csv"
	if mode == "mouse" {
		name = "refreshAttempt"
		for _, item := range items {
			name += item.Name[:min(4, len(item.Name))]
		}
		name += ".csv"
	}
	path := filepath.Join(dir, name)
	newFile := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		newFile = true
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if newFile {
		header := []string{"Time", "Duration", "Refresh count", "Skystone spent", "Gold spent"}
		if mode == "adb" {
			header = []string{"Duration", "Skystone spent", "Gold spent"}
		}
		for _, item := range items {
			header = append(header, item.Name)
		}
		_ = w.Write(header)
	}
	if s.FinishedAt.IsZero() {
		// writeHistory runs from a defer inside the run, before finish()
		// stamps the end time; without this the duration is a zero-value
		// subtraction and prints -2562047h.
		s.FinishedAt = time.Now()
	}
	var row []string
	if mode == "mouse" {
		row = []string{
			s.StartedAt.Format(time.RFC3339),
			s.FinishedAt.Sub(s.StartedAt).Round(time.Second).String(),
			strconv.Itoa(s.RefreshCount), strconv.Itoa(s.Spent), strconv.Itoa(s.Gold),
		}
	} else {
		row = []string{
			s.FinishedAt.Sub(s.StartedAt).Round(time.Millisecond).String(),
			strconv.Itoa(s.Spent), strconv.Itoa(s.Gold),
		}
	}
	for _, item := range items {
		row = append(row, strconv.Itoa(s.Counts[item.ID]))
	}
	_ = w.Write(row)
}
