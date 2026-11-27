import QtQuick
import Quickshell
import Quickshell.Io

// Talks to the e7-refresh control service. The endpoint file holds the
// socket path and a per-user token, on one line each, written by the Go
// daemon. One JSON object goes out and one comes back per command.
Scope {
  id: root

  property var status: ({})
  property var config: ({})
  // Filled from the daemon's "detect" reply: whether a refresh can capture
  // and click the game while it stays hidden, and how.
  property var detect: ({})
  property bool connected: !!sock && sock.connected
  property string socketPath: ""
  property string token: ""

  property int nextId: 1
  property var pending: ({})
  property Socket sock: null

  readonly property string endpointPath: {
    var runtime = Quickshell.env("XDG_RUNTIME_DIR") || ""
    if (runtime === "") runtime = "/run/user/" + uid()
    return runtime + "/e7-refresh/endpoint"
  }

  function uid() {
    var m = /^Uid:\s+(\d+)/m.exec(procStatus.text())
    return m ? m[1] : ""
  }

  FileView {
    id: procStatus
    path: "/proc/self/status"
    blockAllReads: true
    printErrors: false
  }

  // The daemon replaces the endpoint when it restarts, so watch the file
  // and re-arm by reloading it after a change.
  FileView {
    id: endpoint
    path: root.endpointPath
    watchChanges: true
    printErrors: false
    onLoaded: function () {
      var lines = String(text() || "").trim().split("\n")
      if (lines.length < 3 || lines[0] !== "unix") {
        root.socketPath = ""
        root.token = ""
        return
      }
      root.socketPath = lines[1]
      root.token = lines[2]
      root.connectNow()
    }
    onLoadFailed: function () {
      root.socketPath = ""
      root.token = ""
    }
    onFileChanged: reload()
  }

  Component.onCompleted: {
    endpoint.reload()
    poll.start()
  }

  // A failed Quickshell Socket is not reused; make a new one each attempt.
  function connectNow() {
    if (root.socketPath === "") return
    if (sock && sock.connected) return
    if (sock) sock.destroy()
    sock = socketComponent.createObject(root)
    sock.connected = true
  }

  Component {
    id: socketComponent
    Socket {
      path: root.socketPath
      parser: SplitParser {
        onRead: data => root.handle(data)
      }
      onConnectedChanged: {
        if (connected) {
          root.call("get-config", function (msg) {
            if (msg.ok && msg.config) root.config = msg.config
          })
          root.call("status", null)
        }
      }
      onError: function () {}
    }
  }

  function handle(line) {
    if (!line) return
    var msg
    try {
      msg = JSON.parse(line)
    } catch (e) {
      return
    }
    var id = String(msg.id || "")
    var cb = root.pending[id]
    delete root.pending[id]
    if (msg.ok && msg.status) root.status = msg.status
    if (msg.ok && msg.desktop) root.detect = msg.desktop
    if (cb) {
      try {
        cb(msg)
      } catch (e) {}
    }
  }

  function call(cmd, cb, extra) {
    if (!sock || !sock.connected || root.token === "") {
      if (cb) cb({ ok: false, error: "e7-refresh service is not running" })
      return
    }
    var id = String(root.nextId++)
    if (cb) root.pending[id] = cb
    var request = { id: id, token: root.token, cmd: cmd }
    if (extra) {
      for (var key in extra) request[key] = extra[key]
    }
    sock.write(JSON.stringify(request) + "\n")
    sock.flush()
  }

  // Merge a patch into the saved settings and write them atomically.
  function set(patch) {
    var cfg = JSON.parse(JSON.stringify(root.config || {}))
    for (var key in patch) cfg[key] = patch[key]
    root.call("set-config", function (msg) {
      if (msg.ok) root.config = cfg
    }, { config: cfg })
  }

  function setItem(id, checked) {
    var items = JSON.parse(JSON.stringify(root.config.items || {}))
    items[id] = checked
    root.set({ items: items })
  }

  function toggle() {
    if (root.status && root.status.running) root.stop()
    else root.call("start", null)
  }

  function stop() {
    root.call("stop", null, { source: "the panel" })
  }

  function open() {
    root.call("open", null)
  }

  // The settings window is the advanced fallback on Omarchy. Launching the
  // binary again activates the already-running instance through MyGo's
  // single-instance socket, so no daemon call is involved.
  function openSettings() {
    settingsProcess.running = false
    settingsProcess.running = true
  }

  function openHistory() {
    historyProcess.running = false
    historyProcess.running = true
  }

  Process {
    id: settingsProcess
    command: ["e7-refresh", "settings"]
    running: false
  }

  Process {
    id: historyProcess
    command: ["sh", "-c", "xdg-open \"$(e7-refresh history)\""]
    running: false
  }

  Timer {
    id: poll
    interval: 1500
    repeat: true
    running: true
    onTriggered: {
      if (!root.connected) {
        root.connectNow()
        return
      }
      root.call("status", null)
      if (!root.detect || !root.detect.os) root.call("detect", null)
    }
  }
}
