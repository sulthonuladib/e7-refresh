import QtQuick
import Quickshell
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// The E7 Refresh control panel. It is mounted by BarWidget.qml and opens as
// a keyboard panel under the bar icon, the same way Omarchy's own panels do.
// The shell can also summon it with:
//   omarchy-shell shell summon e7-refresh '{}'
Panel {
  id: root
  moduleName: "e7-refresh"
  ipcTarget: "e7-refresh"
  // The bar widget owns the IPC route; this panel is not a separate plugin.
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  readonly property var barIdentity: hostWidget || root

  Backend { id: backend }

  readonly property var detect: backend.detect || ({})
  readonly property bool background: !!detect.backgroundControl
  readonly property var status: backend.status || ({})
  readonly property var config: backend.config || ({})
  readonly property bool running: !!status.running

  function open() {
    backend.call("get-config", null)
    backend.call("status", null)
    root.controller.show()
  }

  function close() {
    root.controller.hide()
  }

  function dismiss() {
    if (shell && typeof shell.hide === "function")
      shell.hide((manifest && manifest.id) || "e7-refresh")
    else close()
  }

  function closeForPopoutSwitch() {
    root.popoutSwitchClosing = true
    close()
    Qt.callLater(function () { root.popoutSwitchClosing = false })
  }

  function apply(patch) {
    backend.set(patch)
  }

  function countsLine() {
    var bought = status.counts || ({})
    var selected = status.items || ({})
    var parts = []
    if (selected.cov) parts.push("Covenant " + (bought.cov || 0))
    if (selected.mys) parts.push("Mystic " + (bought.mys || 0))
    if (selected.fb) parts.push("Friendship " + (bought.fb || 0))
    return parts.length ? parts.join("   ·   ") : "No items selected"
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.barIdentity
    bar: root.bar
    open: root.opened
    centerOnBar: false
    contentWidth: panel.fittedContentWidth(Style.space(430))
    contentHeight: panel.fittedContentHeight(panel.loadedContentHeight, Style.space(700))

    content: PanelKeyCatcher {
      id: catcher
      implicitHeight: Math.min(form.implicitHeight, Style.space(620))
      onCloseRequested: root.dismiss()

      Flickable {
        anchors.fill: parent
        contentWidth: width
        contentHeight: form.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        interactive: contentHeight > height

        Column {
          id: form
          width: parent.width
          spacing: Style.space(12)

          // ── Header ────────────────────────────────────────────────
          Row {
            width: form.width
            spacing: Style.space(10)

            Text {
              anchors.verticalCenter: parent.verticalCenter
              text: "E7 Refresh"
              textFormat: Text.PlainText
              color: Commons.Color.foreground
              font.family: Style.font.family
              font.pixelSize: Style.font.title
              font.bold: true
            }

            Rectangle {
              anchors.verticalCenter: parent.verticalCenter
              visible: root.running
              width: runningText.implicitWidth + Style.space(14)
              height: runningText.implicitHeight + Style.space(6)
              radius: height / 2
              color: Commons.Color.urgent
              Text {
                id: runningText
                anchors.centerIn: parent
                text: "RUNNING"
                textFormat: Text.PlainText
                color: Commons.Color.background
                font.family: Style.font.family
                font.pixelSize: Style.font.caption
                font.bold: true
              }
            }

            Item { width: parent.width - Style.space(46); height: 1 }

            Text {
              anchors.verticalCenter: parent.verticalCenter
              text: "✕"
              textFormat: Text.PlainText
              color: Commons.Color.muted
              font.pixelSize: Style.font.title
              MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: root.dismiss()
              }
            }
          }

          Text {
            width: form.width
            textFormat: Text.PlainText
            color: Commons.Color.muted
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
            text: {
              if (root.running)
                return "Refreshing (" + (root.status.mode || "mouse") + ") · " +
                  (root.status.refreshCount || 0) + " refreshes · " + (root.status.spent || 0) +
                  " skystones"
              if (root.status.lastError) return "Last run failed: " + root.status.lastError
              if (root.status.lastResult) return "Last run: " + root.status.lastResult
              return "Idle"
            }
          }

          // ── Start / stop ──────────────────────────────────────────
          Rectangle {
            width: form.width
            height: Style.space(42)
            radius: Style.cornerRadius
            color: root.running ? Commons.Color.urgent : Commons.Color.accent
            Text {
              anchors.centerIn: parent
              text: root.running ? "Stop refresh" : "Start refresh"
              textFormat: Text.PlainText
              color: Commons.Color.background
              font.family: Style.font.family
              font.pixelSize: Style.font.body
              font.bold: true
            }
            MouseArea {
              anchors.fill: parent
              cursorShape: Qt.PointingHandCursor
              onClicked: backend.toggle()
            }
          }

          PanelSeparator { width: form.width }

          // ── Items ─────────────────────────────────────────────────
          PanelSectionHeader { text: "Items to buy" }

          Toggle {
            width: form.width
            label: "Covenant bookmark"
            checked: !!(root.config.items && root.config.items.cov)
            accent: "#5ea5ff"
            onClicked: backend.setItem("cov", !(root.config.items && root.config.items.cov))
          }
          Toggle {
            width: form.width
            label: "Mystic medal"
            checked: !!(root.config.items && root.config.items.mys)
            accent: "#ff6a6a"
            onClicked: backend.setItem("mys", !(root.config.items && root.config.items.mys))
          }
          Toggle {
            width: form.width
            label: "Friendship bookmark"
            description: "Cheap and safe for checking that matching works"
            checked: !!(root.config.items && root.config.items.fb)
            accent: "#c4a06e"
            onClicked: backend.setItem("fb", !(root.config.items && root.config.items.fb))
          }

          // ── Budget ────────────────────────────────────────────────
          PanelSectionHeader { text: "Skystone budget" }
          Row {
            width: form.width
            spacing: Style.space(10)
            Button {
              text: "−"
              bordered: true
              onClicked: root.apply({ budget: Math.max(0, (root.config.budget || 0) - 100) })
            }
            Text {
              anchors.verticalCenter: parent.verticalCenter
              width: form.width - Style.space(130)
              horizontalAlignment: Text.AlignHCenter
              textFormat: Text.PlainText
              color: Commons.Color.foreground
              font.family: Style.font.family
              font.pixelSize: Style.font.body
              text: (root.config.budget || 0) === 0
                ? "Unlimited" : root.config.budget + " skystones"
            }
            Button {
              text: "+"
              bordered: true
              onClicked: root.apply({ budget: (root.config.budget || 0) + 100 })
            }
          }

          // With no budget the run only ends when it is stopped, which is
          // worth saying out loud next to the "Unlimited" label.
          Text {
            width: form.width
            visible: (root.config.budget || 0) === 0
            text: "Refreshes until you stop it from this panel."
            textFormat: Text.PlainText
            color: Commons.Color.foreground
            opacity: 0.6
            wrapMode: Text.WordWrap
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
          }

          // ── Setup ─────────────────────────────────────────────────
          PanelSectionHeader { text: "Refresh setup" }

          Row {
            width: form.width
            spacing: Style.space(10)
            Text {
              anchors.verticalCenter: parent.verticalCenter
              width: Style.space(108)
              text: "Mode"
              textFormat: Text.PlainText
              color: Commons.Color.foreground
              font.family: Style.font.family
              font.pixelSize: Style.font.body
            }
            ButtonGroup {
              width: form.width - Style.space(118)
              options: [
                { value: "mouse", label: "This desktop" },
                { value: "adb", label: "ADB" }
              ]
              value: root.config.mode || "mouse"
              onChanged: function (value) { root.apply({ mode: value }) }
            }
          }

          // Say what "This desktop" actually does on this machine: on
          // Hyprland an XWayland game is captured and clicked in the
          // background, elsewhere the pointer moves.
          Text {
            width: form.width
            visible: !!root.detect.backgroundMethod && (root.config.mode || "mouse") === "mouse"
            text: root.background
              ? "Runs hidden: " + root.detect.backgroundMethod + "."
              : "Needs the game visible and focused: " + root.detect.backgroundMethod + "."
            textFormat: Text.PlainText
            color: Commons.Color.foreground
            opacity: 0.6
            wrapMode: Text.WordWrap
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
          }

          Row {
            width: form.width
            spacing: Style.space(10)
            Text {
              anchors.verticalCenter: parent.verticalCenter
              width: Style.space(108)
              text: "Game window"
              textFormat: Text.PlainText
              color: Commons.Color.foreground
              font.family: Style.font.family
              font.pixelSize: Style.font.body
            }
            TextField {
              width: form.width - Style.space(118)
              text: root.config.windowTitle || "Epic Seven"
              placeholderText: "Epic Seven"
              onEditingFinished: {
                if (text !== (root.config.windowTitle || "")) root.apply({ windowTitle: text })
              }
            }
          }

          Row {
            width: form.width
            spacing: Style.space(10)
            Text {
              anchors.verticalCenter: parent.verticalCenter
              width: Style.space(108)
              text: "Window size"
              textFormat: Text.PlainText
              color: Commons.Color.foreground
              font.family: Style.font.family
              font.pixelSize: Style.font.body
            }
            NumberField {
              value: root.config.windowWidth || 1280
              from: 640
              to: 2560
              stepSize: 10
              fieldWidth: Style.space(76)
              onModified: function (value) { root.apply({ windowWidth: value }) }
            }
            Text {
              anchors.verticalCenter: parent.verticalCenter
              text: "×"
              textFormat: Text.PlainText
              color: Commons.Color.muted
              font.pixelSize: Style.font.body
            }
            NumberField {
              value: root.config.windowHeight || 720
              from: 360
              to: 1440
              stepSize: 10
              fieldWidth: Style.space(76)
              onModified: function (value) { root.apply({ windowHeight: value }) }
            }
          }

          Row {
            width: form.width
            spacing: Style.space(10)
            Text {
              anchors.verticalCenter: parent.verticalCenter
              width: Style.space(108)
              text: "ADB address"
              textFormat: Text.PlainText
              color: Commons.Color.foreground
              font.family: Style.font.family
              font.pixelSize: Style.font.body
            }
            TextField {
              width: form.width - Style.space(118)
              text: root.config.adbAddress || ""
              placeholderText: "127.0.0.1:5555"
              onEditingFinished: {
                if (text !== (root.config.adbAddress || "")) root.apply({ adbAddress: text })
              }
            }
          }

          PanelSectionHeader { text: "This run" }
          Text {
            width: form.width
            textFormat: Text.PlainText
            color: Commons.Color.foreground
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            text: root.countsLine()
          }

          PanelSeparator { width: form.width }

          Row {
            width: form.width
            spacing: Style.space(10)
            Button {
              text: "Advanced settings"
              bordered: true
              onClicked: backend.openSettings()
            }
            Button {
              text: "Refresh history"
              bordered: true
              onClicked: backend.openHistory()
            }
          }

          Text {
            width: form.width
            text: "Esc to close · Stop refresh ends a run"
            textFormat: Text.PlainText
            color: Commons.Color.muted
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
          }
        }
      }
    }
  }
}
