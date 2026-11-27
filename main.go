// E7 Refresh refreshes the Epic Seven secret shop from the tray or the
// desktop bar. The GUI is optional: `e7-refresh service` runs the same
// engine with no window at all.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/egoist/mygo"

	"github.com/sulthonuladib/e7-refresh/internal/daemon"
	"github.com/sulthonuladib/e7-refresh/internal/engine"
)

//go:embed resources/tray.png
var trayIdle []byte

//go:embed resources/tray-active.png
var trayActive []byte

func main() {
	command := "open"
	if len(os.Args) > 1 {
		command = strings.ToLower(os.Args[1])
	}
	switch command {
	case "open", "":
		runTray()
	case "service":
		runService()
	case "start", "stop", "status", "config", "detect", "history", "version":
		runCLI(command, os.Args[2:])
	case "install-bar":
		installBar()
	case "skill":
		installSkill()
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Printf(`E7 Refresh %s - Epic Seven secret shop refresher

Usage:
  e7-refresh                 start the tray app (default)
  e7-refresh open            start the tray app and show its window
  e7-refresh service         run the headless control service (no GUI)
  e7-refresh start           start a refresh run through the running service
  e7-refresh stop            stop the current run
  e7-refresh status          show refresh status (--json for bar modules)
  e7-refresh config          show settings (--set '<json>' to replace them)
  e7-refresh detect          show the detected desktop and available tools
  e7-refresh history         print the history directory
  e7-refresh install-bar     install the Omarchy bar widget plugin
  e7-refresh skill           install the agent skill in the current folder
  e7-refresh version         print the version

The service is shared by the tray, the bar widget and every command here.
`, daemon.Version)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "e7-refresh:", err)
	os.Exit(1)
}

// requireDisplay turns MyGo's low-level GTK error into an actionable one.
// The headless service has no GUI and never calls this.
func requireDisplay() {
	if runtime.GOOS != "linux" {
		return
	}
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return
	}
	fail(fmt.Errorf(`no graphical session found: DISPLAY and WAYLAND_DISPLAY are both empty.
Start the tray from a terminal inside your desktop session (On Omarchy: Super+Enter),
or run the background service with no GUI:
  e7-refresh service
Then control it with: e7-refresh start | stop | status`))
}

func runService() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Serve(ctx, engine.New(nil)); err != nil {
		fail(err)
	}
}

func runTray() {
	requireDisplay()
	client := daemon.NewClient()
	if !mygo.App.RequestSingleInstanceLock() {
		_, _ = client.Call(daemon.Request{Cmd: "open"})
		return
	}

	var server *daemon.Server
	if !client.Running() {
		s, err := daemon.Start(engine.New(nil))
		if err != nil {
			fmt.Fprintln(os.Stderr, "e7-refresh: service:", err)
		} else {
			server = s
		}
	}
	runApp(client, server)
}
