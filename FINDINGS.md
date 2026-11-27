# Background capture and background clicks on Omarchy/Hyprland — findings

Session: 2026-11-28, Hyprland 0.56.2, one monitor (eDP-1, 1920×1080, scale 1),
Epic Seven running through Steam/Proton as XWayland on workspace 3 while the
user worked on workspace 2.

## Environment for every command below

```sh
export HYPRLAND_INSTANCE_SIGNATURE=$(ls /run/user/$UID/hypr | head -1)
export XDG_RUNTIME_DIR=/run/user/$UID
export WAYLAND_DISPLAY=$(ls /run/user/$UID | grep -m1 '^wayland-[0-9]')
export DISPLAY=:0
```

`hyprctl -j clients` showed the game as:

```
class steam_app_5019180  title "Epic Seven"  xwayland true
stableId 1800043b        at [29,81] size [1280,720]  workspace 3
```

## 1. Capturing a hidden window (works)

```sh
grim -T 1800043b /tmp/e7.png   # 1280x720, ~0.17 s, ~830 KB
```

Captured the live secret shop while the active workspace stayed on 2: `grim -T`
needs the toplevel `stableId`, not the window address. `tesseract` read the
item names out of the capture, so it is the real frame and not a stale buffer.
Two captures ~1 s apart differed by 5–8% of the pixels (shop animations), which
also answers the "does it keep rendering while hidden" question: yes.

Variants tested: `grim -T <id> -` writes the PNG to stdout, which is what the
app uses. A minimized window could not be tested — Hyprland has no core
minimize; there is no observable difference for occluded windows either,
because the compositor renders that toplevel on its own.

## 2. Injecting input into a hidden window

Three channels were tested, with the X pointer warped into the game window so
the injection lands on it. `hyprctl cursorpos`, `hyprctl activewindow` and
`hyprctl activeworkspace` were snapshotted before and after every action, and
the X sprite was restored in a `finally` block.

| Channel | Result |
| --- | --- |
| `XSendEvent` core ButtonPress/Release | delivered by the X server, **ignored by the game** |
| XTEST (`XTestFakeInput`) wheel buttons 4/5 | **ignored** — the shop list did not scroll |
| XTEST press → motion → release (**drag**) | **works** — the list scrolled, 25% of the frame changed, item order in the capture changed |
| XTEST single click (as press/release at one point) | **works** — opened the in-game Help page (89% of the frame changed) |
| XTEST right click | **works** — closed the Help page again |
| XTEST keyboard (`Escape`) | **ignored** — keys need compositor focus, which we deliberately do not take |

The control that made this conclusive: a child X window mapped *inside* the
hidden game window received a `ButtonPress`/`ButtonRelease` from the same
XTEST call, so the transport works and the game's UI decides what it accepts.

Wheel scrolling is the one real gap. The engine scrolls by dragging
(`internal/engine/engine.go`, `scrollShop`), so it does not depend on wheel
events; a future wheel feature would have to use XI2 scroll valuators, not
buttons 4/5.

What is **not** moved by any of this: the physical cursor (`hyprctl cursorpos`
was identical before and after every run), the active window, the active
workspace, and the game window's position. Hyprland does not implement pointer
warp for Xwayland, so `XWarpPointer` only moves the pointer sprite inside the
X server. A pointer warp to (1800,1000) moved the X sprite and left
`hyprctl cursorpos` at (960,525).

## 3. What was implemented

- `internal/platform/input_xwayland_linux.go`: an XTEST input backend built on
  `github.com/jezek/xgb` (pure Go, no cgo, no xdotool). It finds the X window
  by title and `WM_CLASS`, saves and restores the X pointer, jiggles one pixel
  so the game registers hover before the press, then presses and releases.
- `internal/platform/platform_linux.go`: `Background()` on the desktop
  interface. When it is true, `Capture()` is `grim -T <stableId>`,
  `Click`/`Drag` are XTEST at window-local coordinates, `Geometry()` returns
  `{0,0,w,h}`, `MoveResize()` only resizes (and keeps the position), and
  `Activate()` does nothing.
- `internal/engine/engine.go`: skips activation in background mode, closes the
  desktop after a run, and refuses to run on Wayland when the window is not
  background-capable instead of moving the user's cursor.
- `internal/platform/background_linux_test.go`: opt-in proof, used below.

## 4. Verifying it yourself

```sh
# capture only, no click
E7_REFRESH_TEST_WINDOW="Epic Seven" E7_REFRESH_TEST_CLICK=0,0 \
E7_REFRESH_TEST_NO_CLICK=1 E7_REFRESH_TEST_SAVE=/tmp/e7.png \
  go test ./internal/platform -run Background -v

# one background click at a window-local point (282,33 is the shop's help
# button, which opens and closes without touching gold or skystones)
E7_REFRESH_TEST_WINDOW="Epic Seven" E7_REFRESH_TEST_CLICK=282,33 \
  go test ./internal/platform -run Background -v
```

`e7-refresh probe` also runs through the new path: it captured the hidden
window, scored the templates and saved the annotated image without switching
workspace.

## 5. Recommendation

Keep the XWayland background path as the default on Hyprland. It needs `grim`
and nothing else, and it satisfies all four goals from the handoff for this
game. The remaining gaps, in order of importance:

1. **Native Wayland windows** cannot be driven this way; they need a
   compositor-side pointer target (a Hyprland plugin tied to the exact build,
   loaded only with explicit consent) or ADB.
2. **Wheel events** are not delivered to this game at all; use drags.
3. **X11** still uses the old visible-window path. The same `Background()`
   interface can carry an `XSendEvent` backend later without touching the
   engine again; Windows now has one (below).

## 6. Windows: what is possible, and the catch

Windows has no equivalent of XTEST inside Xwayland. There is no second
pointer sprite: `SendInput`, `mouse_event` and `SetCursorPos` all act on the
one system cursor, and a click goes to whatever window is under it. So a
background refresh cannot use real input while the user keeps working.

What it can use instead:

- **Capture**: `PrintWindow(hwnd, hdc, PW_RENDERFULLCONTENT)` asks the window
  to draw itself into a device context. It works for a window that is covered
  or behind others, and returns all black for windows that refuse to render
  (some D3D titles) and for minimized windows. That is probed at run time:
  the window is rendered once, and at least 1% of the sampled pixels have to
  be non-black before the background path is used.
- **Input**: posted window messages — `WM_MOUSEMOVE`, `WM_LBUTTONDOWN`,
  `WM_LBUTTONUP` with client-area coordinates. They go into the window's
  message queue and never touch the cursor, the focus or the z-order. This is
  the direct equivalent of what `XSendEvent` does, and it has the same
  caveat: **the app has to read its message queue**. Unity's Windows player
  handles `WM_` mouse messages, so this game is a reasonable candidate; a game
  that switched to Raw Input (`WM_INPUT`) or DirectInput ignores them.
- Coordinates: `WM_` mouse messages are in client coordinates, so only the
  client area is captured (the client rect is cropped out of the rendered
  frame) and the macro works in that space, exactly like the XWayland path.

Implemented in `internal/platform/platform_windows.go` behind the same
`Background()` interface, so the engine does not have a second code path. If
the probe fails, the old visible-window path (raise the window, move the
pointer) is used; `E7_REFRESH_FOREGROUND=1` forces it even when the probe
succeeds.

**Unverified**: this machine has no Windows install, so the code compiles and
vets for `windows/amd64` but has never driven the game. Verify on Windows
with, in the game's shop:

```
set E7_REFRESH_TEST_WINDOW=Epic Seven
set E7_REFRESH_TEST_SAVE=C:\tmp\e7.png
set E7_REFRESH_TEST_CLICK=282,33
go test ./internal/platform -run Background -v
```

282,33 is the shop's help button: the page opens and closes with a right
click, so nothing is bought. If the capture looks right (the saved PNG is the
shop, not black) but the click does nothing, the game ignores posted messages.
Two levers are left in that case, in order of cost:

1. Post `WM_ACTIVATE`/`WM_SETFOCUS` once at the start of a run, because a few
   games check `GetActiveWindow` before handling input. That is implemented
   and off by default: set `E7_REFRESH_ACTIVATE=1` to try it, since a toolkit
   that trusts those messages can paint itself as focused while the user is
   typing somewhere else.
2. Accept the visible-window path (`E7_REFRESH_FOREGROUND=1`) or ADB. A
   virtual HID driver would give real input, but it drives the same single
   cursor, so it does not solve "keep using the computer".
