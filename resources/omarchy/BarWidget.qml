import QtQuick
import Quickshell
import qs.Commons
import qs.Ui

// The bar widget: purchased counts and spent skystones while a run is live,
// "E7" when idle. Left click opens the control panel, right click starts or
// stops the refresh. The panel is mounted here, inside the bar, so it can
// anchor under this button the way Omarchy's own panels do.
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
      return "E7 Refresh · idle · click for the panel"
    return "E7 Refresh · " + (s.refreshCount || 0) + " refreshes · " + (s.spent || 0) +
      " skystones\n" + (root.counts().join("   ") || "no items selected")
  }

  // The bar routes summon/hide/toggle to this widget, so it has to expose the
  // panel's open state and lifecycle (see omarchy.clock for the contract).
  readonly property bool opened: panelLoader.item ? panelLoader.item.opened === true : false
  readonly property bool popoutSwitchClosing: panelLoader.item ? panelLoader.item.popoutSwitchClosing === true : false

  function open() {
    if (panelLoader.item) panelLoader.item.open()
  }

  function close() {
    if (panelLoader.item) panelLoader.item.close()
  }

  function togglePanel() {
    if (root.opened) root.close()
    else root.open()
  }

  function closeForPopoutSwitch() {
    if (panelLoader.item) panelLoader.item.closeForPopoutSwitch()
  }

  function injectPanel() {
    var target = panelLoader.item
    if (!target) return
    if ("bar" in target) target.bar = root.bar
    if ("settings" in target) target.settings = root.settings
    if ("anchorItem" in target) target.anchorItem = button
    if ("hostWidget" in target) target.hostWidget = root
  }

  onBarChanged: injectPanel()
  onSettingsChanged: injectPanel()

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: {
      root.injectPanel()
      Qt.callLater(root.injectPanel)
    }
  }

  WidgetButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: root.label
    tooltipText: root.tooltipText
    active: root.running
    onPressed: function (mouseButton) {
      // Right click starts a run, but never stops one: stopping costs
      // skystones already spent on the current roll, so it has to be
      // deliberate (the panel's button, or the stop key).
      if (mouseButton === Qt.LeftButton) root.togglePanel()
      else if (backend.status && backend.status.running) root.togglePanel()
      else backend.toggle()
    }
  }
}
