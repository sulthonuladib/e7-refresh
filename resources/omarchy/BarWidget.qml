import QtQuick
import Quickshell
import qs.Commons
import qs.Ui

// The bar widget: active counts and skystones while a run is live, "E7"
// when idle. Left click opens the panel, right click starts or stops.
BarWidget {
  id: root
  moduleName: "e7-refresh"

  Backend { id: backend }

  readonly property var status: backend.status || ({})
  readonly property bool running: !!status.running

  function counts() {
    var selected = status.items || ({})
    var bought = status.counts || ({})
    var parts = []
    if (selected.cov) parts.push("C" + (bought.cov || 0))
    if (selected.mys) parts.push("M" + (bought.mys || 0))
    if (selected.fb) parts.push("F" + (bought.fb || 0))
    return parts
  }

  readonly property string label: {
    if (!root.running) return "E7"
    var parts = root.counts()
    return "E7 " + (parts.length ? parts.join(" ") : "0") + " · " + (status.spent || 0)
  }

  readonly property string tooltipText: {
    var s = root.status
    if (!root.running)
      return "E7 Refresh · idle · click for settings"
    return "E7 Refresh · " + (s.refreshCount || 0) + " refreshes · " + (s.spent || 0) +
      " skystones\n" + (root.counts().join("   ") || "no items selected")
  }

  implicitWidth: labelText.implicitWidth + Style.space(14)
  implicitHeight: barSize

  Text {
    id: labelText
    anchors.centerIn: parent
    text: root.label
    textFormat: Text.PlainText
    color: root.bar ? root.bar.barForeground : Commons.Color.foreground
    font.family: root.bar ? root.bar.fontFamily : Style.fontFamily
    font.pixelSize: Style.font.body
  }

  MouseArea {
    anchors.fill: parent
    hoverEnabled: true
    cursorShape: Qt.PointingHandCursor
    acceptedButtons: Qt.LeftButton | Qt.RightButton | Qt.MiddleButton
    onClicked: function (mouse) {
      if (mouse.button !== Qt.LeftButton) {
        backend.toggle()
        return
      }
      if (root.bar && root.bar.shell && typeof root.bar.shell.toggle === "function")
        root.bar.shell.toggle(root.moduleName, "{}")
      else
        backend.open()
    }
    onEntered: if (root.bar) root.bar.showTooltip(root, root.tooltipText)
    onExited: if (root.bar) root.bar.hideTooltip(root)
  }
}
