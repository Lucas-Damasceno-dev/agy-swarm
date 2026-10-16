import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import "../core"
import "../shared"
import "../services"

PopupWindow {
    id: root

    property color accentColor: ColorScheme.accent
    property color textColor: ColorScheme.text
    property real popupMargin: DesignTokens.spacingLG
    property string selectedModel: "auto"
    property string accountFilter: "ALL"

    anchors.top: true
    anchors.bottom: true
    anchors.left: true
    anchors.right: true

    showScrim: false
    glassRadius: 0
    glassBackground: "transparent"
    glassBorderWidth: 0
    shadowBlur: 0
    originY: 0.0
    originX: 1.0
    slideY: 12

    property bool outsideCloseEnabled: false

    Timer {
        id: closeEnableTimer
        interval: 140
        repeat: false
        onTriggered: root.outsideCloseEnabled = true
    }

    onIsOpenChanged: {
        console.log("[AgyHubPopup] isOpen changed to:", isOpen);
        AgyHubService.isPopupOpen = isOpen;
        if (isOpen) {
            outsideCloseEnabled = false;
            closeEnableTimer.restart();
            AgyHubService.refresh();
        } else {
            outsideCloseEnabled = false;
        }
    }

    // Keyboard Shortcuts (when popup is open)
    Shortcut {
        enabled: root.isOpen
        sequence: "Ctrl+L"
        onActivated: {
            dispatchInput.forceActiveFocus();
            dispatchInput.selectAll();
        }
    }

    Shortcut {
        enabled: root.isOpen && !dispatchInput.activeFocus
        sequence: "1"
        onActivated: AgyHubService.toggleAccountExpanded(0)
    }

    Shortcut {
        enabled: root.isOpen && !dispatchInput.activeFocus
        sequence: "2"
        onActivated: AgyHubService.toggleAccountExpanded(1)
    }

    Shortcut {
        enabled: root.isOpen && !dispatchInput.activeFocus
        sequence: "3"
        onActivated: AgyHubService.toggleAccountExpanded(2)
    }

    Shortcut {
        enabled: root.isOpen && !dispatchInput.activeFocus
        sequence: "4"
        onActivated: AgyHubService.toggleAccountExpanded(3)
    }

    Shortcut {
        enabled: root.isOpen && !dispatchInput.activeFocus
        sequence: "5"
        onActivated: AgyHubService.toggleAccountExpanded(4)
    }

    MouseArea {
        anchors.fill: parent
        enabled: root.isOpen && root.outsideCloseEnabled
        z: 0
        onClicked: function(mouse) {
            var p = mapToItem(popupCard, mouse.x, mouse.y);
            if (p.x < 0 || p.y < 0 || p.x > popupCard.width || p.y > popupCard.height) {
                root.isOpen = false;
            }
        }
    }

    Item {
        id: popupCard
        width: 440
        height: 580
        anchors.top: parent.top
        anchors.right: parent.right
        anchors.topMargin: popupTopMargin
        anchors.rightMargin: root.popupMargin
        z: 1

        Rectangle {
            id: popupBg
            anchors.fill: parent
            radius: DesignTokens.radiusLG
            color: ColorScheme.glassPopup
            border.width: 1
            border.color: ColorScheme.outlineVariant
        }

        Rectangle {
            anchors.fill: popupBg
            anchors.topMargin: 6
            radius: popupBg.radius
            color: Qt.rgba(0, 0, 0, 0.18)
            z: -1
        }

        Flickable {
            id: scrollArea
            anchors.fill: parent
            anchors.margins: DesignTokens.spacingSM
            contentWidth: width
            contentHeight: mainCol.implicitHeight
            boundsBehavior: Flickable.StopAtBounds
            clip: true

            ScrollBar.vertical: ScrollBar {
                policy: scrollArea.contentHeight > scrollArea.height ? ScrollBar.AlwaysOn : ScrollBar.AsNeeded
            }

            ColumnLayout {
                id: mainCol
                width: scrollArea.width - (scrollArea.contentHeight > scrollArea.height ? 10 : 0)
                spacing: DesignTokens.spacingXS

            // ── Header ──────────────────────────────────────────
            RowLayout {
                Layout.fillWidth: true
                spacing: DesignTokens.spacingSM

                Rectangle {
                    width: 34
                    height: 34
                    radius: DesignTokens.radiusSM
                    color: ColorScheme.withAlpha(root.accentColor, 0.18)
                    border.width: 1
                    border.color: ColorScheme.withAlpha(root.accentColor, 0.40)

                    Text {
                        anchors.centerIn: parent
                        text: "\u{f0e7}"
                        font.family: DesignTokens.fontFamilyMono
                        font.pixelSize: 16
                        color: root.accentColor
                    }
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 0

                    Text {
                        text: "AGY-HUB Orchestrator"
                        font.pixelSize: DesignTokens.fontSizeMD
                        font.bold: true
                        font.family: DesignTokens.fontFamilyUI
                        color: root.textColor
                    }

                    Text {
                        text: "Antigravity & Hermes Multi-Account Cluster"
                        font.pixelSize: 10
                        font.family: DesignTokens.fontFamilyUI
                        color: ColorScheme.withAlpha(root.textColor, 0.55)
                    }
                }

                Rectangle {
                    visible: AgyHubService.toastMessage !== ""
                    height: 24
                    radius: 12
                    color: ColorScheme.withAlpha(ColorScheme.green, 0.20)
                    border.width: 1
                    border.color: ColorScheme.withAlpha(ColorScheme.green, 0.40)
                    implicitWidth: toastTxt.implicitWidth + 16

                    Text {
                        id: toastTxt
                        anchors.centerIn: parent
                        text: AgyHubService.toastMessage
                        color: ColorScheme.green
                        font.pixelSize: 10
                        font.bold: true
                    }
                }

                Rectangle {
                    width: 30
                    height: 30
                    radius: DesignTokens.radiusSM
                    color: refMa.containsMouse ? ColorScheme.glassHover : "transparent"
                    border.width: 1
                    border.color: refMa.containsMouse ? ColorScheme.withAlpha(ColorScheme.foreground, DesignTokens.hoverBorderOpacity) : "transparent"

                    Text {
                        anchors.centerIn: parent
                        text: "\u{f021}"
                        font.family: DesignTokens.fontFamilyMono
                        font.pixelSize: 12
                        color: ColorScheme.withAlpha(root.textColor, 0.70)
                    }

                    MouseArea {
                        id: refMa
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: AgyHubService.refresh()
                    }
                }
            }

            // ── Services Strip (CLI-Proxy & Hermes Gateway) ────────
            RowLayout {
                Layout.fillWidth: true
                spacing: DesignTokens.spacingXS

                // CLI-Proxy Card (Clickable to restart)
                Rectangle {
                    Layout.fillWidth: true
                    height: 34
                    radius: DesignTokens.radiusSM
                    color: proxyCardMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.22 : 0.16)
                    border.width: 1
                    border.color: proxyCardMa.containsMouse
                        ? ColorScheme.withAlpha(AgyHubService.proxyRunning ? ColorScheme.green : ColorScheme.red, 0.60)
                        : ColorScheme.withAlpha(ColorScheme.outlineVariant, ColorScheme.isDark ? 0.35 : 0.22)

                    Behavior on color { ColorAnimation { duration: DesignTokens.durationFast } }
                    Behavior on border.color { ColorAnimation { duration: DesignTokens.durationFast } }

                    MouseArea {
                        id: proxyCardMa
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: AgyHubService.restartProxy()
                    }

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 6
                        anchors.rightMargin: 6
                        spacing: 6

                        Rectangle {
                            width: 22
                            height: 22
                            radius: 11
                            color: ColorScheme.withAlpha(AgyHubService.proxyRunning ? ColorScheme.green : ColorScheme.red, 0.18)
                            border.width: 1
                            border.color: ColorScheme.withAlpha(AgyHubService.proxyRunning ? ColorScheme.green : ColorScheme.red, 0.45)

                            Text {
                                anchors.centerIn: parent
                                text: "\u{f0c2}"
                                font.family: DesignTokens.fontFamilyMono
                                font.pixelSize: 10
                                color: AgyHubService.proxyRunning ? ColorScheme.green : ColorScheme.red
                            }
                        }

                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 0

                            RowLayout {
                                spacing: 4
                                Text {
                                    text: "CLI-Proxy"
                                    font.pixelSize: 10
                                    font.bold: true
                                    font.family: DesignTokens.fontFamilyUI
                                    color: root.textColor
                                }
                                Rectangle {
                                    height: 12
                                    radius: 6
                                    implicitWidth: proxyPortTxt.implicitWidth + 6
                                    color: ColorScheme.withAlpha(ColorScheme.blue, 0.20)
                                    border.width: 0.8
                                    border.color: ColorScheme.withAlpha(ColorScheme.blue, 0.40)
                                    Text {
                                        id: proxyPortTxt
                                        anchors.centerIn: parent
                                        text: String(AgyHubService.proxyPort)
                                        font.pixelSize: 7
                                        font.bold: true
                                        color: ColorScheme.blue
                                    }
                                }
                            }

                            Text {
                                text: AgyHubService.isRestartingProxy
                                    ? "REINICIANDO..."
                                    : (AgyHubService.proxyRunning ? ("ON · PID " + AgyHubService.proxyPid + (AgyHubService.proxyRpm > 0 ? (" · " + AgyHubService.proxyRpm + "rpm") : " · RR")) : "OFFLINE")
                                font.pixelSize: 8
                                font.family: DesignTokens.fontFamilyMono
                                color: AgyHubService.isRestartingProxy ? ColorScheme.peach : (AgyHubService.proxyRunning ? ColorScheme.green : ColorScheme.red)
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }
                    }
                }

                // Hermes Gateway Card (Clickable to restart)
                Rectangle {
                    Layout.fillWidth: true
                    height: 34
                    radius: DesignTokens.radiusSM
                    color: hermesCardMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.22 : 0.16)
                    border.width: 1
                    border.color: hermesCardMa.containsMouse
                        ? ColorScheme.withAlpha(AgyHubService.hermesRunning ? ColorScheme.mauve : ColorScheme.red, 0.60)
                        : ColorScheme.withAlpha(ColorScheme.outlineVariant, ColorScheme.isDark ? 0.35 : 0.22)

                    Behavior on color { ColorAnimation { duration: DesignTokens.durationFast } }
                    Behavior on border.color { ColorAnimation { duration: DesignTokens.durationFast } }

                    MouseArea {
                        id: hermesCardMa
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: AgyHubService.restartHermes()
                    }

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 6
                        anchors.rightMargin: 6
                        spacing: 6

                        Rectangle {
                            width: 22
                            height: 22
                            radius: 11
                            color: ColorScheme.withAlpha(AgyHubService.hermesRunning ? ColorScheme.green : ColorScheme.red, 0.18)
                            border.width: 1
                            border.color: ColorScheme.withAlpha(AgyHubService.hermesRunning ? ColorScheme.green : ColorScheme.red, 0.45)

                            Text {
                                anchors.centerIn: parent
                                text: "\u{f1d8}"
                                font.family: DesignTokens.fontFamilyMono
                                font.pixelSize: 10
                                color: AgyHubService.hermesRunning ? ColorScheme.green : ColorScheme.red
                            }
                        }

                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 0

                            RowLayout {
                                spacing: 4
                                Text {
                                    text: "Hermes Agent"
                                    font.pixelSize: 10
                                    font.bold: true
                                    font.family: DesignTokens.fontFamilyUI
                                    color: root.textColor
                                }
                                Rectangle {
                                    height: 12
                                    radius: 6
                                    implicitWidth: hermesModeTxt.implicitWidth + 6
                                    color: ColorScheme.withAlpha(ColorScheme.mauve, 0.20)
                                    border.width: 0.8
                                    border.color: ColorScheme.withAlpha(ColorScheme.mauve, 0.40)
                                    Text {
                                        id: hermesModeTxt
                                        anchors.centerIn: parent
                                        text: "Telegram"
                                        font.pixelSize: 7
                                        font.bold: true
                                        color: ColorScheme.mauve
                                    }
                                }
                            }

                            Text {
                                text: AgyHubService.isRestartingHermes
                                    ? "REINICIANDO..."
                                    : (AgyHubService.hermesRunning ? ("ON · PID " + AgyHubService.hermesPid) : "OFFLINE")
                                font.pixelSize: 8
                                font.family: DesignTokens.fontFamilyMono
                                color: AgyHubService.isRestartingHermes ? ColorScheme.peach : (AgyHubService.hermesRunning ? ColorScheme.green : ColorScheme.red)
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }
                    }
                }
            }

            // ── Hero Cluster Quota Card (Dual Limits: 5h & Weekly) ──────────
            Rectangle {
                Layout.fillWidth: true
                height: 54
                radius: DesignTokens.radiusSM
                color: ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.22 : 0.16)
                border.width: 1
                border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, ColorScheme.isDark ? 0.35 : 0.22)

                RowLayout {
                    anchors.fill: parent
                    anchors.leftMargin: 8
                    anchors.rightMargin: 8
                    spacing: 8

                    // Gemini Cluster Icon
                    Rectangle {
                        width: 32
                        height: 32
                        radius: DesignTokens.radiusSM
                        color: ColorScheme.withAlpha(root.accentColor, 0.18)
                        border.width: 1
                        border.color: ColorScheme.withAlpha(root.accentColor, 0.40)
                        Layout.alignment: Qt.AlignVCenter

                        Text {
                            anchors.centerIn: parent
                            text: "✦"
                            font.pixelSize: 16
                            font.bold: true
                            color: root.accentColor
                        }
                    }

                    // Dual Quota Bars (5h & Weekly)
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 2

                        // Row 1: 5-Hour Limit
                        RowLayout {
                            Layout.fillWidth: true
                            spacing: 4

                            Text {
                                text: "5h (Gemini)"
                                font.pixelSize: 9
                                font.bold: true
                                font.family: DesignTokens.fontFamilyUI
                                color: root.textColor
                                Layout.preferredWidth: 80
                            }

                            Rectangle {
                                Layout.fillWidth: true
                                height: 4
                                radius: 2
                                color: ColorScheme.withAlpha(ColorScheme.surface, 0.35)
                                border.width: 0.5
                                border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.3)

                                Rectangle {
                                    anchors.left: parent.left
                                    anchors.top: parent.top
                                    anchors.bottom: parent.bottom
                                    width: Math.max(4, Math.round(parent.width * Math.max(0.0, Math.min(1.0, AgyHubService.clusterFiveHourPercent / 100.0))))
                                    radius: 2
                                    color: AgyHubService.clusterFiveHourPercent < 30 ? ColorScheme.red : (AgyHubService.clusterFiveHourPercent < 70 ? ColorScheme.peach : ColorScheme.green)
                                }
                            }

                            Text {
                                text: Math.round(AgyHubService.clusterFiveHourPercent) + "%"
                                font.pixelSize: 9
                                font.bold: true
                                font.family: DesignTokens.fontFamilyMono
                                color: AgyHubService.clusterFiveHourPercent < 30 ? ColorScheme.red : (AgyHubService.clusterFiveHourPercent < 70 ? ColorScheme.peach : ColorScheme.green)
                                Layout.preferredWidth: 28
                                horizontalAlignment: Text.AlignRight
                            }
                        }

                        // Row 2: Weekly Limit (Cluster AGY Total)
                        RowLayout {
                            Layout.fillWidth: true
                            spacing: 4

                            Text {
                                text: "Semanal (Cluster AGY)"
                                font.pixelSize: 9
                                font.bold: true
                                font.family: DesignTokens.fontFamilyUI
                                color: ColorScheme.withAlpha(root.textColor, 0.85)
                                Layout.preferredWidth: 80
                                elide: Text.ElideRight
                            }

                            Rectangle {
                                Layout.fillWidth: true
                                height: 4
                                radius: 2
                                color: ColorScheme.withAlpha(ColorScheme.surface, 0.35)
                                border.width: 0.5
                                border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.3)

                                Rectangle {
                                    anchors.left: parent.left
                                    anchors.top: parent.top
                                    anchors.bottom: parent.bottom
                                    width: Math.max(4, Math.round(parent.width * Math.max(0.0, Math.min(1.0, AgyHubService.clusterWeeklyPercent / 100.0))))
                                    radius: 2
                                    color: AgyHubService.clusterWeeklyPercent < 30 ? ColorScheme.red : (AgyHubService.clusterWeeklyPercent < 70 ? ColorScheme.peach : ColorScheme.green)
                                }
                            }

                            Text {
                                text: Math.round(AgyHubService.clusterWeeklyPercent) + "%"
                                font.pixelSize: 9
                                font.bold: true
                                font.family: DesignTokens.fontFamilyMono
                                color: AgyHubService.clusterWeeklyPercent < 30 ? ColorScheme.red : (AgyHubService.clusterWeeklyPercent < 70 ? ColorScheme.peach : ColorScheme.green)
                                Layout.preferredWidth: 28
                                horizontalAlignment: Text.AlignRight
                            }
                        }

                        // Row 3: Telemetry Footer
                        RowLayout {
                            Layout.fillWidth: true
                            spacing: DesignTokens.spacingSM

                            RowLayout {
                                spacing: 3
                                Text {
                                    text: "\u{f017}"
                                    font.family: DesignTokens.fontFamilyMono
                                    font.pixelSize: 8
                                    color: ColorScheme.peach
                                }
                                Text {
                                    readonly property string bpReset: (AgyHubService.bestProfile && AgyHubService.bestProfile.five_hour_reset) ? (" · 5h: " + AgyHubService.bestProfile.five_hour_reset) : ""
                                    text: "Reset: " + AgyHubService.resetCountdown + bpReset
                                    font.pixelSize: 8
                                    font.family: DesignTokens.fontFamilyMono
                                    color: ColorScheme.withAlpha(root.textColor, 0.70)
                                }
                            }

                            Item { Layout.fillWidth: true }

                            RowLayout {
                                spacing: 3
                                Text {
                                    text: "\u{f0ae}"
                                    font.family: DesignTokens.fontFamilyMono
                                    font.pixelSize: 8
                                    color: ColorScheme.withAlpha(root.textColor, 0.5)
                                }
                                Text {
                                    text: AgyHubService.formatSteps(AgyHubService.totalSteps) + " steps"
                                    font.pixelSize: 8
                                    font.family: DesignTokens.fontFamilyMono
                                    color: ColorScheme.withAlpha(root.textColor, 0.70)
                                }
                            }

                            Text {
                                text: "·"
                                font.pixelSize: 8
                                color: ColorScheme.textDisabled
                            }

                            Text {
                                text: AgyHubService.activeJobs + " job(s)"
                                font.pixelSize: 8
                                font.family: DesignTokens.fontFamilyMono
                                font.bold: AgyHubService.activeJobs > 0
                                color: AgyHubService.activeJobs > 0 ? ColorScheme.yellow : ColorScheme.withAlpha(root.textColor, 0.60)
                            }
                        }
                    }
                }
            }

            // ── Cooldown Alert Banner (Visible when any account is quarantined) ──
            Rectangle {
                Layout.fillWidth: true
                height: 32
                radius: DesignTokens.radiusSM
                visible: AgyHubService.cooldownAccounts > 0
                color: ColorScheme.withAlpha(ColorScheme.peach, 0.15)
                border.width: 1
                border.color: ColorScheme.withAlpha(ColorScheme.peach, 0.5)

                RowLayout {
                    anchors.fill: parent
                    anchors.leftMargin: DesignTokens.spacingSM
                    anchors.rightMargin: DesignTokens.spacingSM
                    spacing: 8

                    Text {
                        text: "\u{f132}"
                        font.family: DesignTokens.fontFamilyMono
                        font.pixelSize: 12
                        color: ColorScheme.peach
                    }

                    Text {
                        text: AgyHubService.cooldownAccounts + " conta(s) em quarentena / cooldown"
                        font.pixelSize: 10
                        font.family: DesignTokens.fontFamilyUI
                        font.bold: true
                        color: ColorScheme.peach
                        Layout.fillWidth: true
                    }

                    Rectangle {
                        height: 22
                        radius: 11
                        implicitWidth: unqBannerTxt.implicitWidth + 14
                        color: unqBannerMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.peach, 0.25)
                        border.width: 1
                        border.color: ColorScheme.peach

                        Text {
                            id: unqBannerTxt
                            anchors.centerIn: parent
                            text: "Liberar Agora"
                            font.pixelSize: 9
                            font.bold: true
                            font.family: DesignTokens.fontFamilyUI
                            color: ColorScheme.peach
                        }

                        MouseArea {
                            id: unqBannerMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: AgyHubService.resetCooldown()
                        }
                    }
                }
            }

            // ── Quick Prompt Dispatcher Bar ────────────────────────
            Rectangle {
                id: dispatchBar
                Layout.fillWidth: true
                height: 30
                radius: DesignTokens.radiusSM
                property bool isSwarmMode: false
                color: ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.22 : 0.16)
                border.width: 1
                border.color: dispatchInput.activeFocus ? (dispatchBar.isSwarmMode ? ColorScheme.peach : root.accentColor) : ColorScheme.withAlpha(ColorScheme.outlineVariant, ColorScheme.isDark ? 0.35 : 0.22)
                Behavior on border.color { ColorAnimation { duration: DesignTokens.durationFast } }

                RowLayout {
                    anchors.fill: parent
                    anchors.leftMargin: 6
                    anchors.rightMargin: 4
                    spacing: 4

                    Rectangle {
                        id: modePill
                        height: 18
                        radius: 9
                        implicitWidth: modePillTxt.implicitWidth + 12
                        color: modeMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(dispatchBar.isSwarmMode ? ColorScheme.peach : root.accentColor, 0.20)
                        border.width: 1
                        border.color: ColorScheme.withAlpha(dispatchBar.isSwarmMode ? ColorScheme.peach : root.accentColor, 0.45)

                        RowLayout {
                            anchors.centerIn: parent
                            spacing: 2
                            Text {
                                text: dispatchBar.isSwarmMode ? "🐝" : "⚡"
                                font.pixelSize: 8
                            }
                            Text {
                                id: modePillTxt
                                text: dispatchBar.isSwarmMode ? "Swarm" : "Auto"
                                font.pixelSize: 8
                                font.bold: true
                                font.family: DesignTokens.fontFamilyUI
                                color: dispatchBar.isSwarmMode ? ColorScheme.peach : root.accentColor
                            }
                        }

                        MouseArea {
                            id: modeMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: dispatchBar.isSwarmMode = !dispatchBar.isSwarmMode
                        }
                    }

                    // Model Selector Pill (Auto, Pro, Flash, Sonnet)
                    Rectangle {
                        id: modelPill
                        height: 18
                        radius: 9
                        implicitWidth: modelPillTxt.implicitWidth + 10
                        color: modelMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, 0.35)
                        border.width: 1
                        border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.4)

                        Text {
                            id: modelPillTxt
                            anchors.centerIn: parent
                            text: root.selectedModel === "gemini-3.1-pro-high" ? "💎 Pro" :
                                  (root.selectedModel === "gemini-3.8-flash-high" ? "⚡ Flash" :
                                  (root.selectedModel === "claude-sonnet-4-6" ? "🧠 Sonnet" : "⚡ Auto"))
                            font.pixelSize: 8
                            font.bold: true
                            font.family: DesignTokens.fontFamilyUI
                            color: root.selectedModel === "auto" ? ColorScheme.withAlpha(root.textColor, 0.75) : root.accentColor
                        }

                        MouseArea {
                            id: modelMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (root.selectedModel === "auto") {
                                    root.selectedModel = "gemini-3.1-pro-high";
                                } else if (root.selectedModel === "gemini-3.1-pro-high") {
                                    root.selectedModel = "gemini-3.8-flash-high";
                                } else if (root.selectedModel === "gemini-3.8-flash-high") {
                                    root.selectedModel = "claude-sonnet-4-6";
                                } else {
                                    root.selectedModel = "auto";
                                }
                            }
                        }
                    }

                    TextField {
                        id: dispatchInput
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        placeholderText: dispatchBar.isSwarmMode ? "Despachar em paralelo nas 5 contas..." : "Despachar prompt no worker mais livre..."
                        placeholderTextColor: ColorScheme.withAlpha(root.textColor, 0.45)
                        color: root.textColor
                        font.pixelSize: 10
                        font.family: DesignTokens.fontFamilyUI
                        background: null
                        selectByMouse: true
                        Keys.onUpPressed: (event) => {
                            var prev = AgyHubService.getPreviousPrompt();
                            if (prev !== "") {
                                text = prev;
                                cursorPosition = text.length;
                                event.accepted = true;
                            }
                        }
                        Keys.onDownPressed: (event) => {
                            var nxt = AgyHubService.getNextPrompt();
                            text = nxt;
                            cursorPosition = text.length;
                            event.accepted = true;
                        }
                        onAccepted: {
                            if (text.trim() !== "") {
                                if (dispatchBar.isSwarmMode) {
                                    AgyHubService.dispatchSwarm(text.trim(), root.selectedModel);
                                } else {
                                    AgyHubService.dispatchPrompt(text.trim(), root.selectedModel);
                                }
                                text = "";
                                focus = false;
                            }
                        }
                    }

                    Rectangle {
                        visible: AgyHubService.isDispatching
                        height: 18
                        radius: 9
                        implicitWidth: runTxt.implicitWidth + 12
                        color: ColorScheme.withAlpha(ColorScheme.yellow, 0.18)
                        border.width: 1
                        border.color: ColorScheme.withAlpha(ColorScheme.yellow, 0.45)

                        RowLayout {
                            anchors.centerIn: parent
                            spacing: 3
                            Text {
                                text: "⚡"
                                font.pixelSize: 8
                                color: ColorScheme.yellow
                                SequentialAnimation on opacity {
                                    loops: Animation.Infinite
                                    PropertyAnimation { to: 0.2; duration: 450; easing.type: Easing.InOutQuad }
                                    PropertyAnimation { to: 1.0; duration: 450; easing.type: Easing.InOutQuad }
                                }
                            }
                            Text {
                                id: runTxt
                                text: "Rodando..."
                                font.pixelSize: 7
                                font.bold: true
                                font.family: DesignTokens.fontFamilyUI
                                color: ColorScheme.yellow
                            }
                        }
                    }

                    Rectangle {
                        width: 22
                        height: 22
                        radius: DesignTokens.radiusSM
                        color: sendMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(dispatchBar.isSwarmMode ? ColorScheme.peach : root.accentColor, 0.2)
                        border.width: 1
                        border.color: ColorScheme.withAlpha(dispatchBar.isSwarmMode ? ColorScheme.peach : root.accentColor, 0.4)
                        visible: dispatchInput.text.trim() !== ""

                        Text {
                            anchors.centerIn: parent
                            text: "\u{f1d8}"
                            font.family: DesignTokens.fontFamilyMono
                            font.pixelSize: 10
                            color: dispatchBar.isSwarmMode ? ColorScheme.peach : root.accentColor
                        }

                        MouseArea {
                            id: sendMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (dispatchInput.text.trim() !== "") {
                                    if (dispatchBar.isSwarmMode) {
                                        AgyHubService.dispatchSwarm(dispatchInput.text.trim(), root.selectedModel);
                                    } else {
                                        AgyHubService.dispatchPrompt(dispatchInput.text.trim(), root.selectedModel);
                                    }
                                    dispatchInput.text = "";
                                }
                            }
                        }
                    }
                }
            }

            // ── Account Status Filter Tabs ─────────────────────────
            RowLayout {
                Layout.fillWidth: true
                spacing: 4

                Repeater {
                    model: [
                        { id: "ALL", label: "Todas (" + AgyHubService.activeProfiles.length + ")" },
                        { id: "ACTIVE", label: "Ativas (" + AgyHubService.activeProfiles.filter(p => p.is_running || String(p.status).toUpperCase() === "ACTIVE").length + ")" },
                        { id: "COOLDOWN", label: "Cooldown (" + AgyHubService.activeProfiles.filter(p => p.in_cooldown || String(p.status).toUpperCase() === "COOLDOWN").length + ")" },
                        { id: "ERROR", label: "Erro (" + AgyHubService.activeProfiles.filter(p => String(p.status).toUpperCase() === "ERROR" || !p.email).length + ")" }
                    ]
                    delegate: Rectangle {
                        readonly property bool isSelected: root.accountFilter === modelData.id
                        height: 20
                        radius: DesignTokens.radiusSM - 2
                        implicitWidth: tabTxt.implicitWidth + 12
                        color: isSelected ? ColorScheme.withAlpha(root.accentColor, 0.22) : (tabMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, 0.12))
                        border.width: 1
                        border.color: isSelected ? ColorScheme.withAlpha(root.accentColor, 0.6) : ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.25)

                        Text {
                            id: tabTxt
                            anchors.centerIn: parent
                            text: modelData.label
                            font.pixelSize: 8
                            font.bold: isSelected
                            font.family: DesignTokens.fontFamilyUI
                            color: isSelected ? root.accentColor : ColorScheme.withAlpha(root.textColor, 0.7)
                        }

                        MouseArea {
                            id: tabMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: root.accountFilter = modelData.id
                        }
                    }
                }

                Item { Layout.fillWidth: true }
            }

            // ── Accounts List (5 accounts) ─────────────────────────
            ColumnLayout {
                Layout.fillWidth: true
                spacing: 4

                Repeater {
                    model: AgyHubService.activeProfiles.length
                    delegate: Rectangle {
                        id: accountCard
                        readonly property var profile: AgyHubService.getProfile(index)
                        readonly property string pStatus: profile ? String(profile.status || "") : "IDLE"
                        readonly property color stColor: AgyHubService.getStatusColor(pStatus, accountCard.profile)
                        readonly property string stLabel: AgyHubService.getStatusLabel(pStatus, accountCard.profile)
                        readonly property bool isExpanded: AgyHubService.expandedAccountIndex === index
                        readonly property bool isHovered: accMa.containsMouse
                        readonly property string actTitle: AgyHubService.getActiveConversationTitle(profile)
                        readonly property bool isFilterMatch: {
                            if (root.accountFilter === "ALL") return true;
                            if (root.accountFilter === "ACTIVE") return accountCard.profile && (accountCard.profile.is_running || String(accountCard.pStatus).toUpperCase() === "ACTIVE");
                            if (root.accountFilter === "COOLDOWN") return accountCard.profile && (accountCard.profile.in_cooldown || String(accountCard.pStatus).toUpperCase() === "COOLDOWN");
                            if (root.accountFilter === "ERROR") return accountCard.profile && (String(accountCard.pStatus).toUpperCase() === "ERROR" || !accountCard.profile.email);
                            return true;
                        }

                        visible: isFilterMatch
                        Layout.fillWidth: true
                        implicitHeight: isFilterMatch ? (isExpanded ? Math.max(130, cardContentCol.implicitHeight + 12) : 52) : 0
                        clip: true
                        radius: DesignTokens.radiusSM
                        color: isHovered ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.16 : 0.10)
                        border.width: 1
                        border.color: isHovered
                            ? ColorScheme.withAlpha(root.accentColor, 0.5)
                            : ColorScheme.withAlpha(accountCard.stColor, accountCard.pStatus === "ACTIVE" ? 0.5 : (accountCard.pStatus === "COOLDOWN" ? 0.4 : (ColorScheme.isDark ? 0.25 : 0.18)))

                        Behavior on implicitHeight { NumberAnimation { duration: 160; easing.type: Easing.OutCubic } }
                        Behavior on color { ColorAnimation { duration: DesignTokens.durationFast } }
                        Behavior on border.color { ColorAnimation { duration: DesignTokens.durationFast } }

                        readonly property real p5hPct: (accountCard.profile && accountCard.profile.five_hour_percent !== undefined && accountCard.profile.five_hour_percent > 0) ? accountCard.profile.five_hour_percent : (accountCard.profile ? accountCard.profile.quota_percent : 100.0)
                        readonly property string p5hReset: accountCard.profile ? (accountCard.profile.five_hour_reset || "") : ""
                        readonly property real pWkPct: (accountCard.profile && accountCard.profile.weekly_percent !== undefined && accountCard.profile.weekly_percent > 0) ? accountCard.profile.weekly_percent : 100.0
                        readonly property string pWkReset: accountCard.profile ? (accountCard.profile.weekly_reset || "") : ""
                        readonly property int qSteps: (accountCard.profile && accountCard.profile.remaining_steps !== undefined) ? accountCard.profile.remaining_steps : 2000

                        readonly property color c5hColor: accountCard.pStatus === "COOLDOWN" ? ColorScheme.red : (p5hPct < 30 ? ColorScheme.red : (p5hPct < 70 ? ColorScheme.peach : ColorScheme.green))
                        readonly property color cWkColor: accountCard.pStatus === "COOLDOWN" ? ColorScheme.red : (pWkPct < 30 ? ColorScheme.red : (pWkPct < 70 ? ColorScheme.peach : ColorScheme.green))

                        MouseArea {
                            id: accMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: AgyHubService.toggleAccountExpanded(index)
                        }

                        ColumnLayout {
                            id: cardContentCol
                            anchors.left: parent.left
                            anchors.right: parent.right
                            anchors.top: parent.top
                            anchors.margins: 6
                            spacing: 3

                            // Row 1: Badge, Alias, Priority, Email, Actions, Status Pill
                            RowLayout {
                                Layout.fillWidth: true
                                spacing: 4

                                Rectangle {
                                    width: 16
                                    height: 16
                                    radius: 8
                                    color: ColorScheme.withAlpha(accountCard.stColor, 0.25)
                                    border.width: 1
                                    border.color: accountCard.stColor

                                    Text {
                                        anchors.centerIn: parent
                                        text: String(index + 1)
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 9
                                        font.bold: true
                                        color: accountCard.stColor
                                    }
                                }

                                Text {
                                    text: accountCard.profile ? (accountCard.profile.alias || ("agy-" + (index + 1))) : ("agy-" + (index + 1))
                                    font.family: DesignTokens.fontFamilyMono
                                    font.pixelSize: 10
                                    font.bold: true
                                    color: root.textColor
                                }

                                Rectangle {
                                    readonly property string prioText: (accountCard.profile && accountCard.profile.priority !== undefined) ? ("P" + accountCard.profile.priority) : "P0"
                                    height: 12
                                    radius: 6
                                    implicitWidth: prioTxt.implicitWidth + 6
                                    color: ColorScheme.withAlpha(ColorScheme.foreground, 0.08)
                                    border.width: 0.8
                                    border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.3)
                                    Text {
                                        id: prioTxt
                                        anchors.centerIn: parent
                                        text: parent.prioText
                                        font.pixelSize: 7
                                        font.bold: true
                                        font.family: DesignTokens.fontFamilyMono
                                        color: ColorScheme.withAlpha(root.textColor, 0.75)
                                    }
                                }

                                Text {
                                    text: accountCard.profile ? (accountCard.profile.email || "") : ""
                                    font.family: DesignTokens.fontFamilyUI
                                    font.pixelSize: 9
                                    color: ColorScheme.withAlpha(root.textColor, 0.55)
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                // Quick Unlock button if in COOLDOWN
                                Rectangle {
                                    visible: accountCard.pStatus === "COOLDOWN"
                                    height: 16
                                    radius: 8
                                    implicitWidth: unqAccTxt.implicitWidth + 10
                                    color: unqAccMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.peach, 0.20)
                                    border.width: 1
                                    border.color: ColorScheme.peach

                                    RowLayout {
                                        anchors.centerIn: parent
                                        spacing: 2
                                        Text {
                                            text: "\u{f13e}"
                                            font.family: DesignTokens.fontFamilyMono
                                            font.pixelSize: 8
                                            color: ColorScheme.peach
                                        }
                                        Text {
                                            id: unqAccTxt
                                            text: "Liberar"
                                            font.pixelSize: 7
                                            font.bold: true
                                            color: ColorScheme.peach
                                        }
                                    }
                                    MouseArea {
                                        id: unqAccMa
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: AgyHubService.resetCooldown()
                                    }
                                }

                                // Quick Terminal Launch button
                                Rectangle {
                                    width: 20
                                    height: 16
                                    radius: 3
                                    color: termBtnMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, 0.3)
                                    border.width: 0.8
                                    border.color: termBtnMa.containsMouse ? root.accentColor : ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.4)

                                    Text {
                                        anchors.centerIn: parent
                                        text: ">_"
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 8
                                        font.bold: true
                                        color: termBtnMa.containsMouse ? root.accentColor : ColorScheme.withAlpha(root.textColor, 0.8)
                                    }

                                    MouseArea {
                                        id: termBtnMa
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: {
                                            var a = accountCard.profile ? accountCard.profile.alias : ("agy-" + (index + 1));
                                            AgyHubService.launchProfile(a);
                                            root.isOpen = false;
                                        }
                                    }
                                }

                                // Expand Chevron button
                                Rectangle {
                                    width: 16
                                    height: 16
                                    radius: 8
                                    color: expBtnMa.containsMouse ? ColorScheme.stateHover : "transparent"

                                    Text {
                                        anchors.centerIn: parent
                                        text: accountCard.isExpanded ? "\u{f077}" : "\u{f078}"
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 8
                                        color: ColorScheme.withAlpha(root.textColor, 0.6)
                                    }

                                    MouseArea {
                                        id: expBtnMa
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: AgyHubService.toggleAccountExpanded(index)
                                    }
                                }

                                Rectangle {
                                    height: 16
                                    radius: 8
                                    implicitWidth: statusTxt.implicitWidth + 10
                                    color: ColorScheme.withAlpha(accountCard.stColor, 0.18)
                                    border.width: 1
                                    border.color: ColorScheme.withAlpha(accountCard.stColor, 0.50)

                                    Text {
                                        id: statusTxt
                                        anchors.centerIn: parent
                                        text: accountCard.stLabel
                                        font.family: DesignTokens.fontFamilyUI
                                        font.pixelSize: 8
                                        font.bold: true
                                        color: accountCard.stColor
                                    }
                                }
                            }

                            // Row 2: Compact Metrics (5h mini bar, Weekly mini bar, steps, title)
                            RowLayout {
                                Layout.fillWidth: true
                                spacing: 8

                                // 5-Hour Quota mini
                                RowLayout {
                                    spacing: 3
                                    Text {
                                        text: "5h:"
                                        font.pixelSize: 8
                                        font.bold: true
                                        font.family: DesignTokens.fontFamilyUI
                                        color: ColorScheme.withAlpha(root.textColor, 0.70)
                                    }
                                    Rectangle {
                                        width: 34
                                        height: 3
                                        radius: 1.5
                                        color: ColorScheme.withAlpha(ColorScheme.surface, 0.40)
                                        Rectangle {
                                            anchors.left: parent.left
                                            anchors.top: parent.top
                                            anchors.bottom: parent.bottom
                                            width: Math.max(2, Math.round(parent.width * Math.max(0.0, Math.min(1.0, accountCard.p5hPct / 100.0))))
                                            radius: 1.5
                                            color: accountCard.c5hColor
                                        }
                                    }
                                    Text {
                                        text: Math.round(accountCard.p5hPct) + "%"
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 8
                                        font.bold: true
                                        color: accountCard.c5hColor
                                    }
                                    Text {
                                        text: "⟳" + accountCard.p5hReset
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 7
                                        color: ColorScheme.peach
                                        visible: accountCard.p5hReset !== ""
                                    }
                                }

                                // Weekly Quota mini
                                RowLayout {
                                    spacing: 3
                                    Text {
                                        text: "Sem:"
                                        font.pixelSize: 8
                                        font.bold: true
                                        font.family: DesignTokens.fontFamilyUI
                                        color: ColorScheme.withAlpha(root.textColor, 0.70)
                                    }
                                    Rectangle {
                                        width: 34
                                        height: 3
                                        radius: 1.5
                                        color: ColorScheme.withAlpha(ColorScheme.surface, 0.40)
                                        Rectangle {
                                            anchors.left: parent.left
                                            anchors.top: parent.top
                                            anchors.bottom: parent.bottom
                                            width: Math.max(2, Math.round(parent.width * Math.max(0.0, Math.min(1.0, accountCard.pWkPct / 100.0))))
                                            radius: 1.5
                                            color: accountCard.cWkColor
                                        }
                                    }
                                    Text {
                                        text: Math.round(accountCard.pWkPct) + "%"
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 8
                                        font.bold: true
                                        color: accountCard.cWkColor
                                    }
                                }

                                Text {
                                    text: "~" + accountCard.qSteps + " steps"
                                    font.family: DesignTokens.fontFamilyMono
                                    font.pixelSize: 8
                                    color: ColorScheme.withAlpha(root.textColor, 0.75)
                                }

                                Item { Layout.fillWidth: true }

                                Text {
                                    text: accountCard.actTitle !== "" ? ("\"" + accountCard.actTitle + "\"") : (accountCard.profile ? (accountCard.profile.status_reason || "") : "")
                                    font.family: DesignTokens.fontFamilyUI
                                    font.pixelSize: 8
                                    font.bold: accountCard.actTitle !== ""
                                    color: accountCard.actTitle !== "" ? ColorScheme.yellow : (accountCard.pStatus === "COOLDOWN" ? ColorScheme.red : ColorScheme.withAlpha(root.textColor, 0.45))
                                    elide: Text.ElideRight
                                    Layout.maximumWidth: 110
                                }
                            }

                            // Expanded Drawer
                            Rectangle {
                                Layout.fillWidth: true
                                height: 0.5
                                color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.25)
                                visible: accountCard.isExpanded
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                visible: accountCard.isExpanded
                                opacity: accountCard.isExpanded ? 1.0 : 0.0
                                Behavior on opacity { NumberAnimation { duration: DesignTokens.durationFast } }
                                spacing: 3

                                property string drawerTab: "sessions"

                                // Sub-Tabs Selector: [💬 Sessões] [📜 Logs]
                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: 4

                                    Rectangle {
                                        height: 18
                                        radius: 9
                                        implicitWidth: sessTabTxt.implicitWidth + 10
                                        color: parent.parent.drawerTab === "sessions" ? ColorScheme.withAlpha(root.accentColor, 0.22) : (sessTabMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, 0.15))
                                        border.width: 1
                                        border.color: parent.parent.drawerTab === "sessions" ? ColorScheme.withAlpha(root.accentColor, 0.5) : ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.25)

                                        Text {
                                            id: sessTabTxt
                                            anchors.centerIn: parent
                                            text: "💬 Sessões"
                                            font.pixelSize: 8
                                            font.bold: parent.parent.parent.drawerTab === "sessions"
                                            font.family: DesignTokens.fontFamilyUI
                                            color: parent.parent.parent.drawerTab === "sessions" ? root.accentColor : ColorScheme.withAlpha(root.textColor, 0.65)
                                        }

                                        MouseArea {
                                            id: sessTabMa
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: parent.parent.parent.drawerTab = "sessions"
                                        }
                                    }

                                    Rectangle {
                                        height: 18
                                        radius: 9
                                        implicitWidth: logsTabTxt.implicitWidth + 10
                                        color: parent.parent.drawerTab === "logs" ? ColorScheme.withAlpha(root.accentColor, 0.22) : (logsTabMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, 0.15))
                                        border.width: 1
                                        border.color: parent.parent.drawerTab === "logs" ? ColorScheme.withAlpha(root.accentColor, 0.5) : ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.25)

                                        Text {
                                            id: logsTabTxt
                                            anchors.centerIn: parent
                                            text: "📜 Logs" + ((accountCard.profile && accountCard.profile.recent_log_lines && accountCard.profile.recent_log_lines.length > 0) ? (" (" + accountCard.profile.recent_log_lines.length + ")") : "")
                                            font.pixelSize: 8
                                            font.bold: parent.parent.parent.drawerTab === "logs"
                                            font.family: DesignTokens.fontFamilyUI
                                            color: parent.parent.parent.drawerTab === "logs" ? root.accentColor : ColorScheme.withAlpha(root.textColor, 0.65)
                                        }

                                        MouseArea {
                                            id: logsTabMa
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: parent.parent.parent.drawerTab = "logs"
                                        }
                                    }

                                    Item { Layout.fillWidth: true }

                                    Text {
                                        text: "Tokens 24h: " + AgyHubService.formatTokens(accountCard.profile ? accountCard.profile.est_tokens_24h : 0)
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 8
                                        color: ColorScheme.withAlpha(root.textColor, 0.45)
                                    }
                                }

                                // Interactive Sessions Section
                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 2
                                    visible: parent.drawerTab === "sessions" && accountCard.profile && accountCard.profile.recent_conversations && accountCard.profile.recent_conversations.length > 0

                                    Repeater {
                                        model: (accountCard.profile && accountCard.profile.recent_conversations) ? accountCard.profile.recent_conversations.slice(0, 3) : []
                                        delegate: Rectangle {
                                            id: convRow
                                            Layout.fillWidth: true
                                            height: 18
                                            radius: DesignTokens.radiusSM - 2
                                            color: convMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, 0.10)
                                            border.width: convMa.containsMouse ? 1 : 0
                                            border.color: ColorScheme.withAlpha(root.accentColor, 0.4)

                                            RowLayout {
                                                anchors.fill: parent
                                                anchors.leftMargin: 6
                                                anchors.rightMargin: 6
                                                spacing: 4

                                                Text {
                                                    text: modelData.active ? "●" : "○"
                                                    font.pixelSize: 7
                                                    color: modelData.active ? ColorScheme.green : ColorScheme.withAlpha(root.textColor, 0.35)
                                                }

                                                Text {
                                                    text: (modelData.title && modelData.title !== "") ? modelData.title : (modelData.id ? modelData.id.substring(0, 16) : "Conversa")
                                                    font.family: DesignTokens.fontFamilyUI
                                                    font.pixelSize: 8
                                                    font.bold: modelData.active
                                                    color: convMa.containsMouse ? root.accentColor : (modelData.active ? ColorScheme.yellow : root.textColor)
                                                    elide: Text.ElideRight
                                                    Layout.fillWidth: true
                                                }

                                                Text {
                                                    text: (modelData.step_count || 0) + " steps"
                                                    font.family: DesignTokens.fontFamilyMono
                                                    font.pixelSize: 7
                                                    color: ColorScheme.withAlpha(root.textColor, 0.45)
                                                }

                                                Text {
                                                    text: "↳ Retomar"
                                                    font.family: DesignTokens.fontFamilyUI
                                                    font.pixelSize: 7
                                                    font.bold: true
                                                    color: root.accentColor
                                                    visible: convMa.containsMouse
                                                }
                                            }

                                            MouseArea {
                                                id: convMa
                                                anchors.fill: parent
                                                hoverEnabled: true
                                                cursorShape: Qt.PointingHandCursor
                                                onClicked: {
                                                    var alias = accountCard.profile ? accountCard.profile.alias : ("agy-" + (index + 1));
                                                    AgyHubService.openConversation(alias, modelData.id);
                                                    root.isOpen = false;
                                                }
                                            }
                                        }
                                    }
                                }

                                // Fallback when no recent sessions array
                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: 4
                                    visible: parent.drawerTab === "sessions" && !(accountCard.profile && accountCard.profile.recent_conversations && accountCard.profile.recent_conversations.length > 0)

                                    Text {
                                        text: "\u{f075}"
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 9
                                        color: ColorScheme.withAlpha(root.textColor, 0.5)
                                    }

                                    Text {
                                        text: accountCard.actTitle !== "" ? ("Sessão: " + accountCard.actTitle) : "Nenhuma sessão interativa recente"
                                        font.family: DesignTokens.fontFamilyUI
                                        font.pixelSize: 8
                                        font.italic: accountCard.actTitle === ""
                                        color: accountCard.actTitle !== "" ? ColorScheme.yellow : ColorScheme.withAlpha(root.textColor, 0.5)
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }
                                }

                                // Recent Log Tail Viewer Section
                                Rectangle {
                                    Layout.fillWidth: true
                                    visible: parent.drawerTab === "logs"
                                    implicitHeight: 70
                                    radius: DesignTokens.radiusSM - 2
                                    color: ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.35 : 0.20)
                                    border.width: 0.5
                                    border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.35)
                                    clip: true

                                    Flickable {
                                        anchors.fill: parent
                                        anchors.margins: 4
                                        contentWidth: width
                                        contentHeight: logLinesCol.implicitHeight
                                        boundsBehavior: Flickable.StopAtBounds

                                        ColumnLayout {
                                            id: logLinesCol
                                            width: parent.width
                                            spacing: 1

                                            Repeater {
                                                model: (accountCard.profile && accountCard.profile.recent_log_lines && accountCard.profile.recent_log_lines.length > 0)
                                                    ? accountCard.profile.recent_log_lines.slice(-6)
                                                    : ["(sem logs recentes)"]
                                                delegate: Text {
                                                    Layout.fillWidth: true
                                                    text: modelData
                                                    font.family: DesignTokens.fontFamilyMono
                                                    font.pixelSize: 8
                                                    color: String(modelData).indexOf("ERR") !== -1 || String(modelData).indexOf("429") !== -1
                                                        ? ColorScheme.red
                                                        : (String(modelData).indexOf("WARN") !== -1 ? ColorScheme.peach : ColorScheme.withAlpha(root.textColor, 0.65))
                                                    elide: Text.ElideRight
                                                }
                                            }
                                        }
                                    }
                                }

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: 4
                                    visible: accountCard.profile && accountCard.profile.sparkline !== ""

                                    Text {
                                        text: "24h Activity:"
                                        font.family: DesignTokens.fontFamilyUI
                                        font.pixelSize: 8
                                        color: ColorScheme.withAlpha(root.textColor, 0.5)
                                    }

                                    Text {
                                        text: accountCard.profile ? accountCard.profile.sparkline : ""
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 9
                                        font.bold: true
                                        color: root.accentColor
                                        Layout.fillWidth: true
                                    }

                                    Text {
                                        text: (accountCard.profile ? (accountCard.profile.rpm || 0) : 0) + " rpm · " + (accountCard.profile ? (accountCard.profile.rpd || 0) : 0) + " rpd"
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 8
                                        color: ColorScheme.withAlpha(root.textColor, 0.45)
                                    }
                                }

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: 4

                                    Rectangle {
                                        height: 22
                                        radius: DesignTokens.radiusSM
                                        color: expTermMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(root.accentColor, 0.15)
                                        border.width: 1
                                        border.color: ColorScheme.withAlpha(root.accentColor, 0.40)
                                        implicitWidth: expTermTxt.implicitWidth + 14

                                        RowLayout {
                                            anchors.centerIn: parent
                                            spacing: 4
                                            Text {
                                                text: ">_"
                                                font.family: DesignTokens.fontFamilyMono
                                                font.pixelSize: 8
                                                font.bold: true
                                                color: root.accentColor
                                            }
                                            Text {
                                                id: expTermTxt
                                                text: "Abrir Terminal"
                                                font.family: DesignTokens.fontFamilyUI
                                                font.pixelSize: 8
                                                font.bold: true
                                                color: root.textColor
                                            }
                                        }

                                        MouseArea {
                                            id: expTermMa
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: {
                                                var a = accountCard.profile ? accountCard.profile.alias : ("agy-" + (index + 1));
                                                AgyHubService.launchProfile(a);
                                                root.isOpen = false;
                                            }
                                        }
                                    }

                                    Rectangle {
                                        visible: !!(accountCard.profile && accountCard.profile.email)
                                        height: 22
                                        radius: DesignTokens.radiusSM
                                        color: expCopyMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, 0.15)
                                        border.width: 1
                                        border.color: ColorScheme.withAlpha(root.textColor, 0.25)
                                        implicitWidth: expCopyTxt.implicitWidth + 14

                                        RowLayout {
                                            anchors.centerIn: parent
                                            spacing: 4
                                            Text {
                                                text: "\u{f0c5}"
                                                font.family: DesignTokens.fontFamilyMono
                                                font.pixelSize: 8
                                                color: root.textColor
                                            }
                                            Text {
                                                id: expCopyTxt
                                                text: "Copiar Email"
                                                font.family: DesignTokens.fontFamilyUI
                                                font.pixelSize: 8
                                                color: root.textColor
                                            }
                                        }

                                        MouseArea {
                                            id: expCopyMa
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: AgyHubService.copyToClipboard(accountCard.profile.email)
                                        }
                                    }

                                    Rectangle {
                                        visible: accountCard.pStatus === "ERROR" || (accountCard.profile && !accountCard.profile.email)
                                        height: 22
                                        radius: DesignTokens.radiusSM
                                        color: expAuthMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.red, 0.20)
                                        border.width: 1
                                        border.color: ColorScheme.red
                                        implicitWidth: expAuthTxt.implicitWidth + 14

                                        RowLayout {
                                            anchors.centerIn: parent
                                            spacing: 4
                                            Text {
                                                text: "\u{f084}"
                                                font.family: DesignTokens.fontFamilyMono
                                                font.pixelSize: 8
                                                color: ColorScheme.red
                                            }
                                            Text {
                                                id: expAuthTxt
                                                text: "Reautenticar"
                                                font.family: DesignTokens.fontFamilyUI
                                                font.pixelSize: 8
                                                font.bold: true
                                                color: ColorScheme.red
                                            }
                                        }

                                        MouseArea {
                                            id: expAuthMa
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: {
                                                var pId = accountCard.profile ? accountCard.profile.id : ("account" + (index + 1));
                                                AgyHubService.reauthAccount(pId);
                                                root.isOpen = false;
                                            }
                                        }
                                    }

                                    Rectangle {
                                        visible: accountCard.pStatus === "COOLDOWN"
                                        height: 22
                                        radius: DesignTokens.radiusSM
                                        color: expUnqMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.peach, 0.20)
                                        border.width: 1
                                        border.color: ColorScheme.peach
                                        implicitWidth: expUnqTxt.implicitWidth + 14

                                        RowLayout {
                                            anchors.centerIn: parent
                                            spacing: 4
                                            Text {
                                                text: "\u{f13e}"
                                                font.family: DesignTokens.fontFamilyMono
                                                font.pixelSize: 8
                                                color: ColorScheme.peach
                                            }
                                            Text {
                                                id: expUnqTxt
                                                text: "Liberar Cooldown"
                                                font.family: DesignTokens.fontFamilyUI
                                                font.pixelSize: 8
                                                font.bold: true
                                                color: ColorScheme.peach
                                            }
                                        }

                                        MouseArea {
                                            id: expUnqMa
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: AgyHubService.resetCooldown()
                                        }
                                    }

                                    Item { Layout.fillWidth: true }

                                    Text {
                                        text: "ID: " + (accountCard.profile ? accountCard.profile.id : "")
                                        font.family: DesignTokens.fontFamilyMono
                                        font.pixelSize: 8
                                        color: ColorScheme.withAlpha(root.textColor, 0.35)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // ── Action Buttons ────────────────────────────────────
            RowLayout {
                Layout.fillWidth: true
                spacing: DesignTokens.spacingXS

                // Open TUI Button
                Rectangle {
                    id: tuiBtn
                    Layout.fillWidth: true
                    height: 30
                    radius: DesignTokens.radiusSM
                    color: tuiMa.pressed
                        ? ColorScheme.statePressed
                        : (tuiMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(root.accentColor, 0.16))
                    border.width: 1
                    border.color: ColorScheme.withAlpha(root.accentColor, 0.45)

                    Behavior on color { ColorAnimation { duration: DesignTokens.durationFast } }

                    RowLayout {
                        anchors.centerIn: parent
                        spacing: 4

                        Text {
                            text: "\u{f120}"
                            font.family: DesignTokens.fontFamilyMono
                            font.pixelSize: 11
                            color: root.accentColor
                        }

                        Text {
                            text: "TUI Dashboard"
                            font.family: DesignTokens.fontFamilyUI
                            font.pixelSize: 10
                            font.bold: true
                            color: root.textColor
                        }
                    }

                    MouseArea {
                        id: tuiMa
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            AgyHubService.openTui();
                            root.isOpen = false;
                        }
                    }
                }

                // Trigger Sync Button
                Rectangle {
                    id: syncBtn
                    Layout.fillWidth: true
                    height: 30
                    radius: DesignTokens.radiusSM
                    color: syncMa.pressed
                        ? ColorScheme.statePressed
                        : (syncMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.25 : 0.18))
                    border.width: 1
                    border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.45)

                    Behavior on color { ColorAnimation { duration: DesignTokens.durationFast } }

                    RowLayout {
                        anchors.centerIn: parent
                        spacing: 4

                        Text {
                            text: "\u{f021}"
                            font.family: DesignTokens.fontFamilyMono
                            font.pixelSize: 10
                            color: AgyHubService.isSyncing ? ColorScheme.peach : root.textColor

                            RotationAnimation on rotation {
                                running: AgyHubService.isSyncing
                                from: 0
                                to: 360
                                loops: Animation.Infinite
                                duration: 1000
                            }
                        }

                        Text {
                            text: AgyHubService.isSyncing ? "Sincronizando..." : "Sincronizar"
                            font.family: DesignTokens.fontFamilyUI
                            font.pixelSize: 10
                            font.bold: true
                            color: root.textColor
                        }
                    }

                    MouseArea {
                        id: syncMa
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: AgyHubService.syncAccounts()
                    }
                }

                // Restart Hub Button
                Rectangle {
                    id: restartBtn
                    Layout.fillWidth: true
                    height: 30
                    radius: DesignTokens.radiusSM
                    color: rstMa.pressed
                        ? ColorScheme.statePressed
                        : (rstMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.20 : 0.14))
                    border.width: 1
                    border.color: ColorScheme.withAlpha(ColorScheme.outlineVariant, 0.35)

                    Behavior on color { ColorAnimation { duration: DesignTokens.durationFast } }

                    RowLayout {
                        anchors.centerIn: parent
                        spacing: 4

                        Text {
                            text: "\u{f01e}"
                            font.family: DesignTokens.fontFamilyMono
                            font.pixelSize: 10
                            color: ColorScheme.withAlpha(root.textColor, 0.8)
                        }

                        Text {
                            text: "Reiniciar Hub"
                            font.family: DesignTokens.fontFamilyUI
                            font.pixelSize: 10
                            font.bold: true
                            color: root.textColor
                        }
                    }

                    MouseArea {
                        id: rstMa
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: AgyHubService.restartDaemon()
                    }
                }

                // Reset Cooldown Button (Visible when any account is in cooldown)
                Rectangle {
                    visible: AgyHubService.cooldownAccounts > 0
                    Layout.fillWidth: true
                    height: 30
                    radius: DesignTokens.radiusSM
                    color: rstCdMa.pressed
                        ? ColorScheme.statePressed
                        : (rstCdMa.containsMouse ? ColorScheme.stateHover : ColorScheme.withAlpha(ColorScheme.peach, 0.20))
                    border.width: 1
                    border.color: ColorScheme.peach

                    Behavior on color { ColorAnimation { duration: DesignTokens.durationFast } }

                    RowLayout {
                        anchors.centerIn: parent
                        spacing: 4

                        Text {
                            text: "\u{f13e}"
                            font.family: DesignTokens.fontFamilyMono
                            font.pixelSize: 10
                            color: ColorScheme.peach
                        }

                        Text {
                            text: "Liberar Cooldown"
                            font.family: DesignTokens.fontFamilyUI
                            font.pixelSize: 10
                            font.bold: true
                            color: ColorScheme.peach
                        }
                    }

                    MouseArea {
                        id: rstCdMa
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: AgyHubService.resetCooldown()
                    }
                }
            }

            // ── Footer Stats ──────────────────────────────────────
            RowLayout {
                Layout.fillWidth: true
                spacing: DesignTokens.spacingSM

                Text {
                    text: "Cluster: " + AgyHubService.activeProfiles.length + " contas · Hermes + Proxy"
                    font.family: DesignTokens.fontFamilyUI
                    font.pixelSize: 10
                    color: ColorScheme.withAlpha(root.textColor, 0.55)
                }

                Item { Layout.fillWidth: true }

                Text {
                    text: (AgyHubService.activeJobs > 0 ? (AgyHubService.activeJobs + " jobs ativos · ") : "") + AgyHubService.totalSteps + " steps"
                    font.family: DesignTokens.fontFamilyMono
                    font.pixelSize: 10
                    color: ColorScheme.withAlpha(root.textColor, 0.55)
                }
            }
        }
    }
}
}
