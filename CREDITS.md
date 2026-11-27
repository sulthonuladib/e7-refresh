# Credits and licences

## Artwork

- **App icon and tray icon**: the *Covenant Bookmark* icon from
  [Epic Seven](https://epic7.onstove.com/). Epic Seven and its artwork are
  © Smilegate / Super Creative. The image is used here only to identify the
  tool with the game whose shop it refreshes; this project is unofficial and
  is not affiliated with, endorsed by, or supported by Smilegate.
  The source file was supplied by the project's author
  (`covenant-bookmark-3847276138.png`); the official game page is
  <https://epic7.onstove.com/en/brand>.
- **Bookmark templates** (`resources/assets/cov.png`, `mys.png`, `fb.png`,
  `resources/adb-assets/*`): crops of the same in-game secret shop icons,
  stored and compared by colour so the app can find them on screen. They are
  game artwork, © Smilegate / Super Creative, used for recognition only.
- **Loading image** (`resources/assets/loading.jpg`): a frame of the game's
  load screen, same copyright, used as the app's fallback background.

## Software

- [Epic-Seven-Secret-Shop-Refresh](https://github.com/sya1999/Epic-Seven-Secret-Shop-Refresh)
  by sya1999: the original app this rewrite follows, including its shop
  coordinates, template sizes and CSV history format. GPL-3.0.
- [MyGo](https://github.com/egoist/mygo) by egoist: the Go desktop framework,
  its single-instance forwarding, tray and packaging. MIT.
- [xgb](https://github.com/jezek/xgb) by the XGB authors: pure-Go X11 protocol
  binding, used for XTEST clicks inside Xwayland. BSD-3-Clause.
- [grim](https://git.sr.ht/~emersion/grim): window capture on Wayland, called
  as a program. MIT.
- Omarchy and its Quickshell shell: the bar plugin in `resources/omarchy`
  follows the conventions of Omarchy's own widgets. MIT.

## Research

- The background-input design follows the experiments in `FINDINGS.md`;
  the Hyprland discussion on pointer events for unfocused surfaces
  (<https://github.com/hyprwm/Hyprland/discussions/15377>) and the
  `hyprland-codex-background-computer-use` prototype by Gabriel Kahen
  (<https://github.com/Gabriel-Kahen/hyprland-codex-background-computer-use>)
  were the starting points for `grim -T` and window-targeted X input.

The project itself is GPL-3.0; see [LICENSE](LICENSE).
