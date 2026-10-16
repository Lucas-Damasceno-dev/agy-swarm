import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "../../../core"
import "../../../shared"
import "../../../services"

/**
 * BarAgyHub - Bar module for Antigravity & Hermes Multi-Account Orchestrator.
 * Displays AI sparkle icon with Hermes status badge, 5 account status micro-indicators,
 * dynamic cluster quota summary (5h / weekly) with graceful offline/sync states,
 * tactile press micro-interactions, and a structured monospaced tooltip.
 */
Item {
    id: root

    required property color accentColor
    required property color textColor
    required property var popupInstance
    required property bool anyPopupOpen

    property bool _hovered: false
    property int displayMode: 0

    visible: FeatureFlags.showAgyHub
    implicitHeight: DesignTokens.barItemHeight
    implicitWidth: pillContainer.implicitWidth + (DesignTokens.spacingMD * 2)
    Layout.alignment: Qt.AlignVCenter

    readonly property bool isOpen: ShellController.isPopupOpen(root.popupInstance)
    readonly property bool isClusterOnline: AgyHubService.isClusterReachable
    readonly property bool isSyncing: AgyHubService.isSyncing
    readonly property bool reducedMotion: FeatureFlags.reducedMotion

    // ── Tactile Micro-interaction ───────────────────────────────────────
    transform: Translate {
        y: (ma.pressed && !root.reducedMotion) ? 1 : ((root._hovered && !root.reducedMotion && !root.isOpen) ? -1 : 0)
        Behavior on y {
            enabled: !root.reducedMotion
            NumberAnimation { duration: DesignTokens.durationFast; easing.type: Easing.OutQuad }
        }
    }

    // ── Capsule Background ──────────────────────────────────────────────
    Rectangle {
        id: pillBg
        anchors.fill: parent
        radius: height / 2
        color: root.isOpen
            ? ColorScheme.stateSelected
            : (ma.pressed
                ? ColorScheme.statePressed
                : (root._hovered && !root.anyPopupOpen
                    ? ColorScheme.stateHover
                    : ColorScheme.withAlpha(ColorScheme.surface, ColorScheme.isDark ? 0.16 : 0.12)))
        border.width: 1
        border.color: root.isOpen
            ? ColorScheme.withAlpha(root.accentColor, DesignTokens.activeBorderOpacity)
            : (ma.pressed
                ? ColorScheme.withAlpha(root.accentColor, DesignTokens.activeBorderOpacity)
                : (root._hovered && !root.anyPopupOpen
                    ? ColorScheme.withAlpha(ColorScheme.foreground, DesignTokens.hoverBorderOpacity)
                    : ColorScheme.withAlpha(ColorScheme.outlineVariant, ColorScheme.isDark ? 0.35 : 0.22)))

        Behavior on color {
            enabled: !root.reducedMotion
            ColorAnimation { duration: DesignTokens.durationFast }
        }
        Behavior on border.color {
            enabled: !root.reducedMotion
            ColorAnimation { duration: DesignTokens.durationFast }
        }

        // Low-quota critical pulse animation (<20% 5h quota remaining)
        SequentialAnimation on border.color {
            running: !root.reducedMotion && root.isClusterOnline && AgyHubService.clusterFiveHourPercent > 0 && AgyHubService.clusterFiveHourPercent < 20
            loops: Animation.Infinite
            ColorAnimation { to: ColorScheme.red; duration: 600 }
            ColorAnimation { to: ColorScheme.withAlpha(ColorScheme.red, 0.25); duration: 600 }
        }
    }

    // ── Content Container ───────────────────────────────────────────────
    RowLayout {
        id: pillContainer
        anchors.centerIn: parent
        spacing: DesignTokens.spacingSM

        // AI Sparkle Star + Anchored Hermes Status Badge
        Item {
            id: iconBadgeWrapper
            width: 16
            height: 16
            Layout.alignment: Qt.AlignVCenter

            Text {
                id: geminiStar
                anchors.centerIn: parent
                text: "✦"
                font.family: DesignTokens.fontFamilyUI
                font.pixelSize: DesignTokens.barIconFontSize
                font.bold: true
                renderType: Text.NativeRendering
                color: !root.isClusterOnline
                    ? ColorScheme.textDisabled
                    : (AgyHubService.hasCooldown
                        ? ColorScheme.peach
                        : (AgyHubService.hasError
                            ? ColorScheme.red
                            : (AgyHubService.activeJobs > 0
                                ? ColorScheme.yellow
                                : (AgyHubService.clusterFiveHourPercent < 30 ? ColorScheme.red : root.accentColor))))

                // Smooth organic pulse (750ms) with clean reset to 1.0
                SequentialAnimation on opacity {
                    id: starPulseAnim
                    running: !root.reducedMotion && (AgyHubService.activeJobs > 0 || root.isSyncing || AgyHubService.hasCooldown)
                    loops: Animation.Infinite
                    PropertyAnimation { to: 0.40; duration: 750; easing.type: Easing.InOutQuad }
                    PropertyAnimation { to: 1.0; duration: 750; easing.type: Easing.InOutQuad }
                    onRunningChanged: {
                        if (!running) geminiStar.opacity = 1.0;
                    }
                }

                Behavior on color {
                    enabled: !root.reducedMotion
                    ColorAnimation { duration: DesignTokens.durationFast }
                }
            }

            // Hermes Gateway Micro-Dot (anchored to top-right of star)
            Rectangle {
                width: 5
                height: 5
                radius: 2.5
                visible: AgyHubService.hermesRunning
                color: ColorScheme.green
                anchors.top: parent.top
                anchors.right: parent.right
                anchors.topMargin: -1
                anchors.rightMargin: -3
                border.width: 1
                border.color: pillBg.color
            }

            // CLI-Proxy Micro-Dot (anchored to bottom-right of star)
            Rectangle {
                width: 4.5
                height: 4.5
                radius: 2.25
                visible: AgyHubService.proxyRunning
                color: ColorScheme.blue
                anchors.bottom: parent.bottom
                anchors.right: parent.right
                anchors.bottomMargin: -1
                anchors.rightMargin: -3
                border.width: 1
                border.color: pillBg.color
            }
        }

        // Micro Account Indicators (5 accounts: 5px dots with 3px spacing)
        RowLayout {
            spacing: 3
            Layout.alignment: Qt.AlignVCenter

            Repeater {
                model: AgyHubService.activeProfiles.length
                delegate: Rectangle {
                    id: accountDot
                    readonly property var profile: AgyHubService.getProfile(index)
                    readonly property string pStatus: profile ? String(profile.status || "") : "OFFLINE"
                    readonly property color stColor: AgyHubService.getStatusColor(pStatus, profile)
                    readonly property bool isBusy: profile ? (profile.is_running || pStatus === "ACTIVE") : false
                    readonly property bool isBest: index === AgyHubService.bestProfileIndex && root.isClusterOnline

                    width: isBest ? 6 : 5
                    height: isBest ? 6 : 5
                    radius: isBest ? 3 : 2.5
                    color: stColor
                    opacity: root.isClusterOnline ? 1.0 : 0.45
                    border.width: isBest ? 1 : 0
                    border.color: isBest ? root.accentColor : "transparent"
                    Layout.alignment: Qt.AlignVCenter

                    // Gentle pulse when this specific account is executing
                    SequentialAnimation on opacity {
                        running: !root.reducedMotion && accountDot.isBusy
                        loops: Animation.Infinite
                        PropertyAnimation { to: 0.35; duration: 600; easing.type: Easing.InOutQuad }
                        PropertyAnimation { to: 1.0; duration: 600; easing.type: Easing.InOutQuad }
                        onRunningChanged: {
                            if (!running) accountDot.opacity = root.isClusterOnline ? 1.0 : 0.45;
                        }
                    }

                    Behavior on color {
                        enabled: !root.reducedMotion
                        ColorAnimation { duration: DesignTokens.durationFast }
                    }
                }
            }
        }

        // Active Alert Badge (Cooldown / Error Count)
        Rectangle {
            visible: (AgyHubService.hasCooldown || AgyHubService.hasError) && root.isClusterOnline
            height: 12
            radius: 6
            color: AgyHubService.hasError ? ColorScheme.withAlpha(ColorScheme.red, 0.25) : ColorScheme.withAlpha(ColorScheme.peach, 0.25)
            border.width: 0.8
            border.color: AgyHubService.hasError ? ColorScheme.red : ColorScheme.peach
            implicitWidth: cdBadgeTxt.implicitWidth + 6
            Layout.alignment: Qt.AlignVCenter

            Text {
                id: cdBadgeTxt
                anchors.centerIn: parent
                text: AgyHubService.hasError ? "!" : String(AgyHubService.cooldownAccounts)
                font.family: DesignTokens.fontFamilyMono
                font.pixelSize: 8
                font.bold: true
                color: AgyHubService.hasError ? ColorScheme.red : ColorScheme.peach
            }
        }

        // Quotas or State Label (SYNC... / OFF / 5h% · Wk%)
        RowLayout {
            spacing: 3
            Layout.alignment: Qt.AlignVCenter

            // When syncing:
            Text {
                visible: root.isSyncing
                text: "SYNC"
                font.family: DesignTokens.fontFamilyUI
                font.pixelSize: DesignTokens.barLabelFontSize
                font.bold: true
                renderType: Text.NativeRendering
                color: ColorScheme.peach
                Layout.alignment: Qt.AlignVCenter
            }

            // When completely offline:
            Text {
                visible: !root.isSyncing && !root.isClusterOnline
                text: "OFF"
                font.family: DesignTokens.fontFamilyUI
                font.pixelSize: DesignTokens.barLabelFontSize
                font.bold: true
                renderType: Text.NativeRendering
                color: ColorScheme.textDisabled
                opacity: 0.75
                Layout.alignment: Qt.AlignVCenter
            }

            // Dynamic display modes for quota & metrics
            RowLayout {
                visible: !root.isSyncing && root.isClusterOnline
                spacing: 3
                Layout.alignment: Qt.AlignVCenter

                // Mode 0: Cluster Average (5h% · Wk%)
                RowLayout {
                    visible: root.displayMode === 0
                    spacing: 3
                    Layout.alignment: Qt.AlignVCenter

                    Text {
                        readonly property int h5Val: Math.round(AgyHubService.clusterFiveHourPercent) || 0
                        text: h5Val + "%"
                        font.family: DesignTokens.fontFamilyUI
                        font.pixelSize: DesignTokens.barLabelFontSize
                        font.bold: true
                        font.features: { "tnum": 1 }
                        renderType: Text.NativeRendering
                        color: h5Val < 30 ? ColorScheme.red : (h5Val < 70 ? ColorScheme.peach : root.textColor)
                        opacity: 0.95
                        Layout.alignment: Qt.AlignVCenter
                    }

                    Text {
                        text: "·"
                        font.family: DesignTokens.fontFamilyUI
                        font.pixelSize: DesignTokens.barLabelFontSize
                        font.bold: true
                        renderType: Text.NativeRendering
                        color: ColorScheme.outline
                        Layout.alignment: Qt.AlignVCenter
                    }

                    Text {
                        readonly property int wkVal: Math.round(AgyHubService.clusterWeeklyPercent) || 0
                        text: wkVal + "%"
                        font.family: DesignTokens.fontFamilyUI
                        font.pixelSize: DesignTokens.barLabelFontSize
                        font.bold: true
                        font.features: { "tnum": 1 }
                        renderType: Text.NativeRendering
                        color: wkVal < 30 ? ColorScheme.red : (wkVal < 70 ? ColorScheme.peach : ColorScheme.textSecondary)
                        opacity: 0.85
                        Layout.alignment: Qt.AlignVCenter
                    }
                }

                // Mode 1: Best Available Account (e.g. "agy3 71%")
                RowLayout {
                    visible: root.displayMode === 1
                    spacing: 3
                    Layout.alignment: Qt.AlignVCenter

                    Text {
                        readonly property var bp: AgyHubService.bestProfile
                        readonly property int bpVal: bp ? (Math.round(bp.five_hour_percent) || Math.round(bp.quota_percent) || 0) : 0
                        text: (bp ? (bp.alias || "best") : "best") + " " + bpVal + "%"
                        font.family: DesignTokens.fontFamilyUI
                        font.pixelSize: DesignTokens.barLabelFontSize
                        font.bold: true
                        font.features: { "tnum": 1 }
                        renderType: Text.NativeRendering
                        color: bpVal < 30 ? ColorScheme.red : (bpVal < 70 ? ColorScheme.peach : root.accentColor)
                        Layout.alignment: Qt.AlignVCenter
                    }
                }

                // Mode 2: Bottleneck Min 5h Quota (e.g. "min 12%")
                RowLayout {
                    visible: root.displayMode === 2
                    spacing: 3
                    Layout.alignment: Qt.AlignVCenter

                    Text {
                        readonly property int minVal: Math.round(AgyHubService.minFiveHourPercent) || 0
                        text: "min " + minVal + "%"
                        font.family: DesignTokens.fontFamilyUI
                        font.pixelSize: DesignTokens.barLabelFontSize
                        font.bold: true
                        font.features: { "tnum": 1 }
                        renderType: Text.NativeRendering
                        color: minVal < 30 ? ColorScheme.red : (minVal < 70 ? ColorScheme.peach : ColorScheme.green)
                        Layout.alignment: Qt.AlignVCenter
                    }
                }

                // Mode 3: Realtime Activity Throughput (e.g. "20k · 0rpm")
                RowLayout {
                    visible: root.displayMode === 3
                    spacing: 3
                    Layout.alignment: Qt.AlignVCenter

                    Text {
                        text: AgyHubService.formatSteps(AgyHubService.totalSteps) + (AgyHubService.proxyRpm > 0 ? (" · " + AgyHubService.proxyRpm + "rpm") : "")
                        font.family: DesignTokens.fontFamilyMono
                        font.pixelSize: DesignTokens.barLabelFontSize
                        font.bold: true
                        font.features: { "tnum": 1 }
                        renderType: Text.NativeRendering
                        color: AgyHubService.activeJobs > 0 ? ColorScheme.yellow : ColorScheme.withAlpha(root.textColor, 0.8)
                        Layout.alignment: Qt.AlignVCenter
                    }
                }
            }
        }
    }

    // ── Mouse & Interaction Handling ───────────────────────────────────
    MouseArea {
        id: ma
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        acceptedButtons: Qt.LeftButton | Qt.RightButton | Qt.MiddleButton

        onEntered: root._hovered = true
        onExited: root._hovered = false

        onWheel: (wheel) => {
            root.displayMode = (root.displayMode + (wheel.angleDelta.y > 0 ? 1 : 3)) % 4;
        }

        onClicked: (mouse) => {
            if (mouse.button === Qt.LeftButton) {
                console.log("[BarAgyHub] Clicked left button! popupInstance:", root.popupInstance);
                ShellController.togglePopup(root, root.popupInstance);
            } else if (mouse.button === Qt.RightButton) {
                if (!root.isClusterOnline) {
                    AgyHubService.restartDaemon();
                } else {
                    AgyHubService.syncAccounts();
                }
            } else if (mouse.button === Qt.MiddleButton) {
                AgyHubService.openTui();
            }
        }
    }

    // ── Rich Monospaced Tooltip ─────────────────────────────────────────
    readonly property string richTooltip: {
        var lines = [];
        lines.push("✦ AGY-HUB // Google AI Cluster Orchestrator");
        lines.push("──────────────────────────────────────────");
        if (!root.isClusterOnline) {
            lines.push("Status: CLUSTER OFFLINE");
            lines.push("Execute 'agy-hub' ou clique para abrir painel");
            lines.push("──────────────────────────────────────────");
            lines.push("• Botão Direito: Iniciar Cluster Daemon");
        } else {
            lines.push("🌐 Cluster 5-Horas:  " + (Math.round(AgyHubService.clusterFiveHourPercent) || 0) + "% restante");
            lines.push("📅 Cluster Semanal:  " + (Math.round(AgyHubService.clusterWeeklyPercent) || 0) + "% restante");
            lines.push("──────────────────────────────────────────");
            for (var i = 0; i < AgyHubService.activeProfiles.length; i++) {
                var p = AgyHubService.getProfile(i);
                if (p) {
                    var st = AgyHubService.getStatusLabel(p.status, p);
                    var alias = String(p.alias || ("agy-" + (i + 1)));
                    var h5 = (p.five_hour_percent !== undefined) ? (Math.round(p.five_hour_percent) + "%") : "--";
                    var h5Reset = p.five_hour_reset ? (" (" + p.five_hour_reset + ")") : "";
                    var wk = (p.weekly_percent !== undefined) ? (Math.round(p.weekly_percent) + "%") : "--";
                    var tok = AgyHubService.formatTokens(p.est_tokens_24h || 0);
                    var actConv = AgyHubService.getActiveConversationTitle(p);
                    var convInfo = actConv ? (" [\"" + actConv + "\"]") : "";
                    lines.push("• " + alias + " [" + st + "]: 5h " + h5 + h5Reset + " | Sem " + wk + " | " + tok + convInfo);
                }
            }
            lines.push("──────────────────────────────────────────");
            lines.push("Proxy (127.0.0.1:" + AgyHubService.proxyPort + "): " + (AgyHubService.proxyRunning ? ("ONLINE (PID " + AgyHubService.proxyPid + ")") : "OFFLINE"));
            lines.push("Hermes Gateway: " + (AgyHubService.hermesRunning ? ("ONLINE (PID " + AgyHubService.hermesPid + ")") : "OFFLINE"));
            lines.push("");
            lines.push("• Clique: Abrir painel de cotas & controle");
            lines.push("• Botão Direito: Sincronizar contas (sync)");
        }
        lines.push("• Botão Meio: Abrir TUI Dashboard");
        return lines.join("\n");
    }

    ToolTip {
        visible: root._hovered && !root.isOpen
        text: root.richTooltip
        delay: DesignTokens.tooltipDelayMs
        timeout: DesignTokens.tooltipTimeoutMs
        font.family: DesignTokens.fontFamilyMono
    }
}
