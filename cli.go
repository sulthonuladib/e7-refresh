package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sulthonuladib/e7-refresh/internal/config"
	"github.com/sulthonuladib/e7-refresh/internal/daemon"
)

func flag(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runCLI(command string, args []string) {
	client := daemon.NewClient()
	switch command {
	case "version":
		fmt.Println(daemon.Version)

	case "status":
		response, err := client.Call(daemon.Request{Cmd: "status"})
		if err != nil {
			fail(err)
		}
		if hasFlag(args, "--json") {
			if err := printJSON(response.Status); err != nil {
				fail(err)
			}
			return
		}
		s := response.Status
		if s.Running {
			fmt.Printf("refreshing (%s): %d refreshes, %d skystones, %d gold\n",
				s.Mode, s.RefreshCount, s.Spent, s.Gold)
		} else {
			fmt.Printf("idle: %s\n", s.LastResult)
		}
		if s.Window != "" {
			fmt.Printf("window: %s\n", s.Window)
		}
		if s.LastError != "" {
			fmt.Printf("last error: %s\n", s.LastError)
		}

	case "start":
		req := daemon.Request{Cmd: "start"}
		if value := flag(args, "--config"); value != "" {
			cfg, err := readConfig(value)
			if err != nil {
				fail(err)
			}
			req.Config = &cfg
		}
		if _, err := client.Call(req); err != nil {
			fail(err)
		}
		fmt.Println("refresh started")

	case "stop":
		if _, err := client.Call(daemon.Request{Cmd: "stop", Source: "the command line"}); err != nil {
			fail(err)
		}
		fmt.Println("refresh stopping")

	case "config":
		if value := flag(args, "--set"); value != "" {
			// Merge into the saved settings instead of replacing them: a
			// one-key patch used to wipe the window title and every other
			// field it did not mention.
			data, err := readConfigFile(value)
			if err != nil {
				fail(err)
			}
			current, err := client.Call(daemon.Request{Cmd: "get-config"})
			if err != nil {
				fail(err)
			}
			if current.Config == nil {
				fail(fmt.Errorf("the service did not return the saved settings"))
			}
			cfg := mergeConfig(*current.Config, data)
			if _, err := client.Call(daemon.Request{Cmd: "set-config", Config: &cfg}); err != nil {
				fail(err)
			}
			fmt.Println("settings saved")
			return
		}
		response, err := client.Call(daemon.Request{Cmd: "get-config"})
		if err != nil {
			fail(err)
		}
		if hasFlag(args, "--json") {
			if err := printJSON(response.Config); err != nil {
				fail(err)
			}
			return
		}
		cfg := *response.Config
		fmt.Printf("mode: %s\nwindow: %s\nbudget: %d\nitems: %s\n",
			cfg.Mode, cfg.WindowTitle, cfg.Budget, strings.Join(enabledNames(cfg), ", "))
		fmt.Printf("config file: %s/config.json\n", config.Dir())

	case "detect":
		response, err := client.Call(daemon.Request{Cmd: "detect"})
		if err != nil {
			fail(err)
		}
		if hasFlag(args, "--json") {
			if err := printJSON(response.Desktop); err != nil {
				fail(err)
			}
			return
		}
		d := response.Desktop
		fmt.Printf("os: %s (%s)\nsession: %s\ndesktop: %s\nbar: %s\n",
			d.OS, d.Distro, d.Session, d.Desktop, d.Bar)
		input := d.InputTool
		if d.BackgroundControl {
			// The pointer-moving tool is only a fallback once the window can
			// be driven in the background.
			input += " (visible-window fallback)"
		}
		fmt.Printf("appindicator: %t\nscreenshot tool: %s\ninput tool: %s\n",
			d.HasAppIndicator, d.ScreenshotTool, input)
		label := "no"
		if d.BackgroundControl {
			label = "yes"
		}
		fmt.Printf("background control: %s (%s)\n", label, d.BackgroundMethod)

	case "history":
		response, err := client.Call(daemon.Request{Cmd: "history"})
		if err != nil {
			fail(err)
		}
		fmt.Println(response.Info["dir"])

	case "tray":
		value := "on"
		if len(args) > 0 {
			value = args[0]
		}
		if value != "on" && value != "off" {
			fail(fmt.Errorf("usage: e7-refresh tray on|off"))
		}
		if _, err := client.Call(daemon.Request{Cmd: "tray", Value: value}); err != nil {
			fail(err)
		}
		fmt.Printf("tray %s\n", value)
	}
}

func readConfigFile(value string) ([]byte, error) {
	data := []byte(value)
	if _, err := os.Stat(value); err == nil {
		read, err := os.ReadFile(value)
		if err != nil {
			return nil, err
		}
		data = read
	}
	return data, nil
}

func readConfig(value string) (config.Config, error) {
	data, err := readConfigFile(value)
	if err != nil {
		return config.Config{}, err
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, fmt.Errorf("invalid settings JSON: %w", err)
	}
	cfg.Normalize()
	return cfg, nil
}

func enabledNames(cfg config.Config) []string {
	var names []string
	for _, item := range config.Items {
		if cfg.Items[item.ID] {
			names = append(names, item.Name)
		}
	}
	return names
}

// mergeConfig applies the keys a patch actually contains over the saved
// settings. Unmarshalling into the saved struct leaves every absent field
// alone, so `config --set '{"budget":300}'` changes one field instead of
// wiping the window title and the rest.
func mergeConfig(saved config.Config, data []byte) config.Config {
	if err := json.Unmarshal(data, &saved); err != nil {
		return saved
	}
	saved.Normalize()
	return saved
}
