import QtQuick
import Quickshell
import qs.Commons
import qs.Ui

// The Omarchy panel for E7 Refresh. It follows the same contract as the
// shell's own panels: the host injects shell/manifest and calls open/close.
Item {
  id: root

  property var shell: null
  property var manifest: null
  property bool opened: false

  Backend { id: backend }

  function open(payloadJson) {
    opened = true
    backend.call("get-config", null)
    backend.call("status", null)
  }

  function close() { opened = false }

  function dismiss() {
    if (shell && typeof shell.hide === "function")
      shell.hide((manifest && manifest.id) || "e7-refresh")
    else close()
  }

  Keys.onEscapePressed: root.dismiss()

  implicitWidth: 380
  implicitHeight: 480

  Rectangle {
    anchors.fill: parent
    color: Commons.Color.background
    radius: Style.cornerRadius
    border.width: 1
    border.color: Qt.rgba(Commons.Color.foreground.r, Commons.Color.foreground.g, Commons.Color.foreground.b, 0.12)

    Column {
      anchors.fill: parent
      anchors.margins: Style.space(18)
      spacing: Style.space(12)

      Row {
        width: parent.width
        spacing: Style.space(10)

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: "E7 Refresh"
          color: Commons.Color.foreground
          font.family: Style.fontFamily
          font.pixelSize: Style.font.title
          font.bold: true
        }

        Item { width: parent.width - closeButton.width - Style.space(10); height: 1 }

        Text {
          id: closeButton
          anchors.verticalCenter: parent.verticalCenter
          text: "✕"
          color: Commons.Color.muted
          font.pixelSize: Style.font.body
          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: root.dismiss()
          }
        }
      }

      Text {
        width: parent.width
        text: backend.status && backend.status.running
          ? "Refreshing · " + (backend.status.refreshCount || 0) + " refreshes · " + (backend.status.spent || 0) + " skystones"
          : "Idle"
        color: Commons.Color.muted
        font.family: Style.fontFamily
        font.pixelSize: Style.font.caption
      }

      Rectangle {
        width: parent.width
        height: 40
        radius: Style.cornerRadius
        color: backend.status && backend.status.running ? Commons.Color.urgent : Commons.Color.accent
        Text {
          anchors.centerIn: parent
          text: backend.status && backend.status.running ? "Stop refresh" : "Start refresh"
          color: Commons.Color.background
          font.family: Style.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }
        MouseArea {
          anchors.fill: parent
          cursorShape: Qt.PointingHandCursor
          onClicked: backend.toggle()
        }
      }

      Text {
        text: "Items to buy"
        color: Commons.Color.foreground
        font.family: Style.fontFamily
        font.pixelSize: Style.font.subtitle
        font.bold: true
      }

      Toggle {
        id: covenant
        width: parent.width
        label: "Covenant bookmark"
        checked: !!(backend.config.items && backend.config.items.cov)
        accent: "#5ea5ff"
        onClicked: backend.setItem("cov", !covenant.checked)
      }

      Toggle {
        id: mystic
        width: parent.width
        label: "Mystic medal"
        checked: !!(backend.config.items && backend.config.items.mys)
        accent: "#ff6a6a"
        onClicked: backend.setItem("mys", !mystic.checked)
      }

      Toggle {
        id: friendship
        width: parent.width
        label: "Friendship bookmark"
        checked: !!(backend.config.items && backend.config.items.fb)
        accent: "#c4a06e"
        onClicked: backend.setItem("fb", !friendship.checked)
      }

      Row {
        width: parent.width
        spacing: Style.space(10)
        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: "Skystone budget"
          color: Commons.Color.foreground
          font.family: Style.fontFamily
          font.pixelSize: Style.font.body
        }
        Item { width: parent.width - budgetValue.width - minus.width - plus.width - 2 * parent.spacing; height: 1 }
        Text {
          id: minus
          text: "−"
          color: Commons.Color.accent
          font.pixelSize: Style.font.title
          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: backend.set({ budget: Math.max(0, (backend.config.budget || 0) - 100) })
          }
        }
        Text {
          id: budgetValue
          text: backend.config.budget || "0"
          color: Commons.Color.foreground
          font.family: Style.fontFamily
          font.pixelSize: Style.font.body
        }
        Text {
          id: plus
          text: "+"
          color: Commons.Color.accent
          font.pixelSize: Style.font.title
          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: backend.set({ budget: (backend.config.budget || 0) + 100 })
          }
        }
      }

      Rectangle {
        width: parent.width
        height: 1
        color: Qt.rgba(Commons.Color.foreground.r, Commons.Color.foreground.g, Commons.Color.foreground.b, 0.12)
      }

      Text {
        width: parent.width
        text: {
          var s = backend.status || ({})
          var bought = s.counts || ({})
          var selected = s.items || ({})
          var parts = []
          if (selected.cov) parts.push("Covenant " + (bought.cov || 0))
          if (selected.mys) parts.push("Mystic " + (bought.mys || 0))
          if (selected.fb) parts.push("Friendship " + (bought.fb || 0))
          return parts.length ? parts.join("  ·  ") : "No items selected"
        }
        color: Commons.Color.foreground
        font.family: Style.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }

      Text {
        width: parent.width
        text: "Esc to close · right-click the bar icon to start or stop"
        color: Commons.Color.muted
        font.family: Style.fontFamily
        font.pixelSize: Style.font.caption
      }
    }
  }
}
