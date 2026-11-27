# E7 Secret Shop Refresh (Go / MyGo)

A cross-platform rewrite of [Epic-Seven-E7-Secret-Shop-Refresh](https://github.com/sya1999/Epic-Seven-Secret-Shop-Refresh)
in Go with [MyGo](https://github.com/egoist/mygo). It refreshes the Epic Seven
secret shop from the **system tray** or the **desktop bar** instead of living
in a permanent window, and runs the same engine as a **headless service**.

```
e7-refresh                 # opens the app window, starts the tray and service
e7-refresh service         # headless background service, no display required
e7-refresh start           # start a refresh from the shell or a bar script
e7-refresh status --json   # machine-readable state for bars and agents
e7-refresh stop            # stop after the current action
e7-refresh install-service # install and enable the systemd user service
e7-refresh remove-service  # disable and remove it
e7-refresh tray off|on     # hide or show the tray icon
```

Opening the app from the launcher or application menu shows its window; the
tray icon and bar widget are controls, not the only way in. The tray menu and
settings window both have toggles for the tray icon and for the background
service. `install-bar` is hidden once the Omarchy widget is installed.

## What it does

Two modes, carried over from the upstream app:

| Mode | How it works | Needs |
| --- | --- | --- |
| `mouse` | Finds the emulator window, screenshots it, template-matches the bookmark icons, then clicks buy/scroll/refresh | the emulator visible on screen; `xdotool` + a screenshot tool on X11, `grim` + `ydotool` on Wayland |
| `adb` | Uses `adb exec-out screencap -p` and `adb shell input tap/swipe` | ADB enabled on the emulator, resolution set to 1920×1080, `adb` on PATH |

Covenant and mystic bookmarks stay enabled; friendship bookmarks are optional
and are the safest way to check that matching works before leaving it alone.
Selection, match sensitivity and an optional folder of custom templates are
all editable in the app window and the Omarchy panel. Templates are matched
across several nearby scales, so a Steam/Proton client rendering the icons at
a different pixel size still works. Each run appends a CSV row to
`~/.local/share/e7-refresh/ShopRefreshHistory`.

**Stop anytime:** press `Esc` (or the configured stop key) anywhere in the
session. On Linux the app reads keyboard event devices directly, so this works
on X11 and Wayland without a compositor shortcut. The tray menu, bar widget,
settings window and `e7-refresh stop` work too.

## Build

Go 1.27.3+ is the only build requirement; MyGo is pure Go, so Linux and
Windows binaries can be cross-compiled from one machine.

```sh
./bin/build                    # quick host binary in build/dev/
./bin/build-all                # Linux + Windows packages in build/<os>-<arch>/
make install                   # Arch Linux: build PKGBUILD and makepkg -fsi
```

`bin/build-all` produces, per Linux architecture, a `.deb`, an `.rpm`, a
PKGBUILD, a tarball with `install.sh`, and a desktop entry. Windows builds
produce `e7-refresh.exe`; set `WITH_INSTALLER=1` with NSIS installed to also
build the setup executable. Releases are manual: tag the commit, then
`./bin/release v0.1.0` creates a draft GitHub release with the artifacts.
CI for automatic builds and releases comes later.

## Linux

### Session protocols

The app detects `XDG_SESSION_TYPE`, `WAYLAND_DISPLAY` and the desktop, and
picks the right helpers:

- **X11**: `xdotool` for windows and mouse, `import` / `scrot` / `maim` for
  screenshots. The window can be moved to the top-left and resized to
  906×539 automatically, like the upstream program.
- **Wayland**: `grim` for screenshots and `ydotool` for input. On Hyprland,
  `hyprctl` additionally provides window geometry and focus, using the Lua
  dispatcher syntax of Hyprland 0.56+ with a fallback for older releases. On
  compositors without window IPC, focus the emulator yourself and set
  `E7_REFRESH_WINDOW_RECT=x,y,w,h` if the macros need explicit coordinates.
- **Tray**: MyGo uses AppIndicator / StatusNotifierItem over D-Bus, which
  works on X11 and Wayland desktops that support it (GNOME with its
  AppIndicator extension, KDE Plasma natively, and panels with an SNI
  module). Missing tools are reported in the window, not silently skipped.

Check what was detected with `e7-refresh detect`.

| Distribution | Tray package | Screenshot / input |
| --- | --- | --- |
| Arch / Omarchy | `libayatana-appindicator` | `imagemagick`, `xdotool`, `grim`, `ydotool` |
| Debian / Ubuntu | `libayatana-appindicator3-1` | `imagemagick`, `scrot`, `xdotool`, `grim`, `ydotool` |
| Fedora / RHEL | `libayatana-appindicator-gtk3` | `imagemagick`, `xdotool`, `grim`, `ydotool` |
| openSUSE | `libayatana-appindicator3-1` | `imagemagick`, `xdotool`, `grim`, `ydotool` |

Install with the artifact for your distro:

```sh
# Debian / Ubuntu
sudo apt install ./build/linux-amd64/e7-refresh_0.1.0_amd64.deb
# Fedora / RHEL
sudo rpm -i build/linux-amd64/e7-refresh-0.1.0-1.x86_64.rpm
# Arch / Omarchy
cd build/linux-amd64 && makepkg -fsi
# Any distro, no root
sh build/linux-amd64/install.sh
```

### Omarchy

Omarchy's Quickshell bar does not use AppIndicator, so the app ships a QML
bar plugin. Install it from the app or the shell:

```sh
e7-refresh install-bar
omarchy restart shell   # only if the widget does not appear
```

The widget shows `E7` when idle; while refreshing it shows bought counts for
the selected kinds (C/M/F) and spent skystones, e.g. `E7 C2 M1 · 21`. Left
click opens a settings panel in the Omarchy shell with item selection, budget
and live stats; right click starts or stops. Omarchy uses this panel instead
of a separate overlay window. The installer copies files to
`~/.config/omarchy/plugins/e7-refresh`, backs up `shell.json`, and never edits
`/usr/share/omarchy`.

### Other desktops and bars

GNOME and KDE use the AppIndicator tray. waybar and polybar scripts are in
[`integrations/`](integrations/README.md), as is a systemd user unit for
running the headless service at login.

## Windows

Build on any machine with `./bin/build-all`, then copy
`build/windows-amd64/E7 Refresh.exe` together with its `assets/`,
`adb-assets/` and `omarchy/` folders to the target, or build the NSIS
installer with `WITH_INSTALLER=1`. The installer
puts the app in `%LOCALAPPDATA%\Programs\E7 Refresh`, adds a Start menu
shortcut, and does not need administrator rights. The tray icon shows in the
notification area. The Win32/GDI backend finds the emulator window, captures
the virtual screen and injects mouse events without extra tools.

## Settings

Settings live in `~/.config/e7-refresh/config.json`, written atomically.
They can be edited from the tray menu, the settings window, or the CLI:

```sh
e7-refresh config --json
e7-refresh config --set '{"mode":"adb","budget":1000,"items":{"cov":true,"mys":true,"fb":false}}'
```

| Setting | Default | Meaning |
| --- | --- | --- |
| `mode` | `mouse` | `mouse` or `adb` |
| `windowTitle` | `Epic Seven` | Exact window title for mouse mode |
| `items` | cov + mys | Which bookmarks to buy |
| `budget` | `0` | Skystone budget; 0 runs until stopped |
| `mouseSleep` / `screenshotSleep` | `0.3` | Delays between mouse actions and screenshots |
| `tapSleep` | `0.3` | Delay between ADB taps |
| `allowMove` | `false` | Leave the emulator where it is instead of moving it to 0,0 |
| `randomOffset` | `false` | Small jitter on ADB taps |
| `adbAddress` | `127.0.0.1:5555` | Device to connect |
| `stopHotkey` | `Escape` | Session-wide stop key, read from keyboard devices on Linux |
| `hint` | `true` | Small bought-items overlay on non-Omarchy desktops |
| `matchThreshold` | `0.62` | Lower to detect icons rendered slightly differently |
| `templateDir` | empty | Folder containing your own `cov.png`, `mys.png`, `fb.png` crops |

Press the stop shortcut, choose **Stop refresh**, or run `e7-refresh stop`
to finish after the current action. Always verify that the program recognises
a bookmark before leaving it unsupervised, and keep the display on in mouse
mode. The original game files are never modified.

## Agent skill

`e7-refresh skill` installs a short skill in the current project's
`.agents/skills/e7-refresh/` so agents can check status, start and stop runs,
change settings and find history without opening the UI.

## Troubleshooting

`e7-refresh: mygo: cannot open display (is DISPLAY or WAYLAND_DISPLAY set?)`
means the **GUI command** was started from a shell outside the graphical
session (SSH, an agent shell, cron, or a service unit). The tray and settings
window need a display. Run it from a terminal inside the desktop session
(Omarchy: `Super+Enter`), or use the headless service instead:

```sh
e7-refresh service            # no display needed
e7-refresh start              # from any shell
e7-refresh status --json
```

Check your session with `echo $WAYLAND_DISPLAY $DISPLAY`; on Omarchy/Hyprland
both should normally be set (`wayland-*` and `:0`). For systemd use
`integrations/e7-refresh.service`, which already starts the `service`
subcommand.

## License

GPL-3.0, inherited from the upstream project. See [LICENSE](LICENSE).
