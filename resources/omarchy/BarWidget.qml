import QtQuick
import Quickshell
import qs.Commons
import qs.Ui

// The bar widget: "E7" while idle, "E7 <refreshes>" while refreshing.
// Left click starts or stops a run, middle click opens the settings window.
BarWidget {
  id: root
  moduleName: "e7-refresh"

  Backend { id: backend }

  readonly property var status: backend.status || ({})
  readonly property bool running: !!status.running
  readonly property string label: running ? ("E7 " + (status.refreshCount || 0)) : "E7"
  readonly property string tooltipText: running
    ? "E7 Refresh · refreshing · " + (status.refreshCount || 0) + " refreshes · " + (status.spent || 0) + " skystones"
    : "E7 Refresh · idle · click to start"

  implicitWidth: row.implicitWidth + Style.space(12)
  implicitHeight: barSize

  Row {
    id: row
    anchors.centerIn: parent
    spacing: Style.space(6)

    Text {
      text: root.running ? "▶" : "■"
      color: root.running && root.bar ? root.bar.barForeground : Qt.darker(root.bar ? root.bar.barForeground : "white", 1.4)
      font.family: root.bar ? root.bar.fontFamily : ""
      font.pixelSize: Style.font.body
      anchors.verticalCenter: parent.verticalCenter
    }

    Text {
      text: root.label
      color: root.bar ? root.bar.barForeground : "white"
      font.family: root.bar ? root.bar.fontFamily : ""
      font.pixelSize: Style.font.body
      anchors.verticalCenter: parent.verticalCenter
    }
  }

  MouseArea {
    anchors.fill: parent
    hoverEnabled: true
    cursorShape: Qt.PointingHandCursor
    acceptedButtons: Qt.LeftButton | Qt.MiddleButton
    onClicked: function (mouse) {
      if (mouse.button === Qt.MiddleButton) backend.open()
      else backend.toggle()
    }
    onEntered: if (root.bar) root.bar.showTooltip(root, root.tooltipText)
    onExited: if (root.bar) root.bar.hideTooltip(root)
  }
}
