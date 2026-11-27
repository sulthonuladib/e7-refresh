# Desktop integrations

Every integration talks to the same local control service. Start the service
once (the tray app starts it automatically if needed):

```sh
e7-refresh service &
```

## Omarchy

`e7-refresh install-bar` copies the QML plugin into
`~/.config/omarchy/plugins/e7-refresh` and adds `e7-refresh` to the bar's
right section in `~/.config/omarchy/shell.json`. The JSON file is backed up
first; nothing under `/usr/share/omarchy` is touched. Launching the app does
the same on first run, and the shell hot-reloads the plugin and layout. If the
widget does not appear:

```sh
omarchy restart shell
```

The widget shows `E7` when idle and bought counts plus spent skystones while
refreshing (for example `E7 C2 M1 · 21`). Left click opens the control panel;
right click starts or stops. There is no tray
icon on Omarchy; **Advanced settings** in the panel opens the full settings
window when you need the fields the panel does not show.

## GNOME and KDE Plasma

MyGo's tray uses the AppIndicator / StatusNotifierItem protocol, which GNOME
shows with its AppIndicator extension and Plasma shows natively. Install
`libayatana-appindicator3` (Arch) or `libayatana-appindicator3-1` (Debian and
Ubuntu). The tray works on both X11 and Wayland sessions.

## Other Wayland compositors (Hyprland, Sway, wlroots)

Use a bar that supports JSON modules. The waybar module is in
`integrations/waybar/e7-refresh.sh`; copy it and add a `custom/e7` block as
described at the top of the script. For mouse refresh on wlroots:

- `grim` takes screenshots, `ydotool` injects mouse events.
- On Hyprland the app additionally uses `hyprctl` for window geometry and
  focus, so the emulator can be moved and activated.
- On other compositors focus the emulator yourself and, if needed, set
  `E7_REFRESH_WINDOW_RECT=x,y,w,h` so the macros know where it is.

## X11 desktops (XFCE, i3, Openbox, ...)

The tray needs `libayatana-appindicator3`. Mouse refresh uses `xdotool` and
`import`/`scrot`. Polybar users can use `integrations/polybar/e7-refresh.sh`.

## Windows

The MyGo tray icon lives in the notification area. A Win32/GDI backend finds
the emulator window, captures the virtual screen and sends mouse events; no
external tools or Python are needed. The installer puts `e7-refresh.exe` in
`%LOCALAPPDATA%\Programs\E7 Refresh` with a Start menu shortcut.

## Running without a GUI

Copy `integrations/e7-refresh.service` to
`~/.config/systemd/user/e7-refresh.service`, then:

```sh
systemctl --user daemon-reload
systemctl --user enable --now e7-refresh.service
```

Control it from any terminal with `e7-refresh start`, `stop` and `status`.
