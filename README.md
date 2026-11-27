# E7 Refresh

Buys the covenant and mystic bookmarks in the Epic Seven secret shop, and
refreshes the shop as often as your budget allows. It lives in the system tray
or the desktop bar, and on Omarchy/Hyprland it works on a window you never
have to look at: no cursor movement, no focus stealing, no workspace switch.

![E7 Refresh](resources/icon.png)

## Install

Linux, no root:

```sh
curl -fsSL https://raw.githubusercontent.com/sulthonuladib/e7-refresh/main/install.sh | sh
```

That puts the app in `~/.local/e7-refresh.app`, adds `E7 Refresh` to your
applications menu with its icon, and leaves your settings alone. Remove it
again with `sh install.sh --uninstall`.

Or use the package for your distribution from
[Releases](https://github.com/sulthonuladib/e7-refresh/releases):

| System | File |
| --- | --- |
| Debian, Ubuntu | `e7-refresh_0.2.0_amd64.deb` → `sudo apt install ./…deb` |
| Fedora, RHEL | `e7-refresh-0.2.0-1.x86_64.rpm` → `sudo rpm -i …rpm` |
| Arch, Omarchy | `makepkg -fsi` in the release folder (from `PKGBUILD`) |
| Windows | `E7 Refresh-setup-0.2.0-amd64.exe`, or the portable `.exe` with its `assets/` folder |

## Use

Open **E7 Refresh** from the applications menu. On Omarchy, click the `E7`
widget in the bar instead: the panel is the whole app. Pick the bookmarks you
want, set a skystone budget (or leave it unlimited), press **Start refresh**.

- **Unlimited** keeps refreshing until you press **Stop refresh** in the panel
  (or Stop in the tray menu). Stop only happens when you ask for it.
- A budget stops by itself once it is spent, and says so in the last-run line.
- The bar widget shows what it bought and what it spent while it runs.

`Epic Seven` has to be running. On Hyprland it can sit on any workspace, even
behind other windows: the panel's "This desktop" mode captures and clicks the
game through XWayland without touching your pointer or keyboard.

| Desktop | How a refresh drives the game |
| --- | --- |
| Omarchy / Hyprland | Hidden: `grim -T` capture and XTEST clicks inside Xwayland |
| GNOME, KDE, X11 | The game window is raised and resized; the pointer moves |
| Other Wayland compositors | `grim` + `ydotool`, window visible and focused |
| Windows | The window is shown and the pointer moves |
| Android emulator | `ADB` mode, no window handling at all |

Missing tools are listed in the panel (`grim`, `xdotool`, `ydotool`,
`adb` — see [integrations/README.md](integrations/README.md) for bar scripts and a systemd unit).

## Development

```sh
./bin/build      # dev binary in build/dev
./bin/build-all  # packages and installers for every platform
```

Pushing a `v*` tag builds and publishes the release in CI.

`FINDINGS.md` documents the experiments behind the background mode.

## Credits

The icon is the covenant bookmark from Epic Seven, © Smilegate / Super
Creative; this project is unofficial and not affiliated with them. Everything
else — the upstream app this is a rewrite of, MyGo, xgb, grim, Omarchy — is
listed in [CREDITS.md](CREDITS.md). GPL-3.0, see [LICENSE](LICENSE).
