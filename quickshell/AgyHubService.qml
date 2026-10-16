pragma Singleton
import QtQuick
import Quickshell
import Quickshell.Io
import "../core"

Item {
    id: root

    property var profiles: []
    property bool proxyRunning: false
    property int proxyPort: 8317
    property int proxyPid: 0
    property int proxyRpm: 0
    property bool hermesRunning: false
    property int hermesPid: 0
    property string hermesStatus: "OFFLINE"
    property string resetCountdown: "--"
    property int totalSteps: 0
    property int activeJobs: 0
    property string lastUpdated: ""
    property bool isLoading: false
    property bool isSyncing: false
    property int clusterQuotaTokens: 12500000
    property int clusterRemainingTokens: 12500000
    property int clusterQuotaPercent: 0
    property real clusterFiveHourPercent: 0
    property real clusterWeeklyPercent: 0
    property string lastError: ""
    property string toastMessage: ""
    property bool isPopupOpen: false
    property bool isDispatching: false
    property bool isRestartingProxy: false
    property bool isRestartingHermes: false
    property bool isRestartingDaemon: false

    readonly property string binPath: RuntimePaths.joinPath(RuntimePaths.homeDir, ".local/bin/agy-hub")

    readonly property bool hasCooldown: {
        if (!activeProfiles) return false;
        for (var i = 0; i < activeProfiles.length; i++) {
            if (activeProfiles[i] && String(activeProfiles[i].status).toUpperCase() === "COOLDOWN") return true;
        }
        return false;
    }

    readonly property int cooldownAccounts: {
        if (!activeProfiles) return 0;
        var count = 0;
        for (var i = 0; i < activeProfiles.length; i++) {
            if (activeProfiles[i] && String(activeProfiles[i].status).toUpperCase() === "COOLDOWN") count++;
        }
        return count;
    }

    readonly property bool hasError: {
        if (!activeProfiles) return false;
        for (var i = 0; i < activeProfiles.length; i++) {
            if (activeProfiles[i] && String(activeProfiles[i].status).toUpperCase() === "ERROR") return true;
        }
        return false;
    }

    readonly property var fallbackProfiles: [
        { "alias": "agy-1", "id": "default", "email": "account1@cluster.local", "status": "OFFLINE", "total_steps": 0, "est_tokens_24h": 0, "rpm": 0, "priority": 50, "proxy_synced": false, "status_reason": "Desconectado" },
        { "alias": "agy-2", "id": "account2", "email": "account2@cluster.local", "status": "OFFLINE", "total_steps": 0, "est_tokens_24h": 0, "rpm": 0, "priority": 10, "proxy_synced": false, "status_reason": "Desconectado" },
        { "alias": "agy-3", "id": "account3", "email": "account3@cluster.local", "status": "OFFLINE", "total_steps": 0, "est_tokens_24h": 0, "rpm": 0, "priority": 100, "proxy_synced": false, "status_reason": "Desconectado" },
        { "alias": "agy-4", "id": "account4", "email": "account4@cluster.local", "status": "OFFLINE", "total_steps": 0, "est_tokens_24h": 0, "rpm": 0, "priority": 90, "proxy_synced": false, "status_reason": "Desconectado" },
        { "alias": "agy-5", "id": "account5", "email": "account5@cluster.local", "status": "OFFLINE", "total_steps": 0, "est_tokens_24h": 0, "rpm": 0, "priority": 80, "proxy_synced": false, "status_reason": "Desconectado" }
    ]

    readonly property var activeProfiles: (profiles && profiles.length > 0) ? profiles : fallbackProfiles

    readonly property bool isClusterReachable: (profiles && profiles.length > 0) && (lastError === "")

    readonly property int bestProfileIndex: {
        if (!activeProfiles || activeProfiles.length === 0) return 0;
        var bestIdx = 0;
        var bestScore = -999999;
        for (var i = 0; i < activeProfiles.length; i++) {
            var p = activeProfiles[i];
            if (!p) continue;
            if (p.in_cooldown || String(p.status).toUpperCase() === "COOLDOWN" || String(p.status).toUpperCase() === "ERROR") continue;
            var wPct = (p.weekly_percent !== undefined) ? p.weekly_percent : 50;
            var hPct = (p.five_hour_percent !== undefined) ? p.five_hour_percent : 50;
            var score = (wPct * 2.0) + hPct;
            if (p.is_running || String(p.status).toUpperCase() === "ACTIVE") score -= 150;
            if (p.priority !== undefined) score += (p.priority * 0.5);
            if (score > bestScore) {
                bestScore = score;
                bestIdx = i;
            }
        }
        return bestIdx;
    }

    readonly property var bestProfile: getProfile(bestProfileIndex)

    readonly property real minFiveHourPercent: {
        if (!activeProfiles || activeProfiles.length === 0) return 0;
        var m = 100;
        for (var i = 0; i < activeProfiles.length; i++) {
            var p = activeProfiles[i];
            if (p && p.five_hour_percent !== undefined) {
                if (p.five_hour_percent < m) m = p.five_hour_percent;
            }
        }
        return m;
    }

    property var promptHistory: []
    property int promptHistoryIndex: -1

    function pushPromptHistory(promptText) {
        var p = String(promptText || "").trim();
        if (!p) return;
        var hist = promptHistory.slice();
        if (hist.length === 0 || hist[hist.length - 1] !== p) {
            hist.push(p);
            if (hist.length > 40) hist.shift();
            promptHistory = hist;
        }
        promptHistoryIndex = -1;
    }

    function getPreviousPrompt() {
        if (!promptHistory || promptHistory.length === 0) return "";
        if (promptHistoryIndex === -1) {
            promptHistoryIndex = promptHistory.length - 1;
        } else if (promptHistoryIndex > 0) {
            promptHistoryIndex--;
        }
        return promptHistory[promptHistoryIndex] || "";
    }

    function getNextPrompt() {
        if (!promptHistory || promptHistory.length === 0 || promptHistoryIndex === -1) return "";
        if (promptHistoryIndex < promptHistory.length - 1) {
            promptHistoryIndex++;
            return promptHistory[promptHistoryIndex];
        } else {
            promptHistoryIndex = -1;
            return "";
        }
    }

    function getProfile(index) {
        if (activeProfiles && index >= 0 && index < activeProfiles.length) {
            return activeProfiles[index];
        }
        return null;
    }

    function getActiveConversationTitle(profile) {
        if (!profile || !profile.recent_conversations || !Array.isArray(profile.recent_conversations)) return "";
        for (var i = 0; i < profile.recent_conversations.length; i++) {
            var c = profile.recent_conversations[i];
            if (c && c.active && c.title) return String(c.title).trim();
        }
        return "";
    }

    function getStatusColor(status, profile) {
        if (!root.isClusterReachable && !root.isSyncing) {
            return ColorScheme.textDisabled;
        }
        if (profile) {
            if (profile.in_cooldown || String(status).toUpperCase() === "COOLDOWN") return ColorScheme.red;
            if (profile.status === "ERROR" || String(status).toUpperCase() === "ERROR") return ColorScheme.red;
            if (profile.is_running || String(status).toUpperCase() === "ACTIVE") return ColorScheme.yellow;
            if ((profile.five_hour_percent !== undefined && profile.five_hour_percent < 25) || profile.remaining_tokens === 0) {
                return ColorScheme.peach;
            }
        }
        var s = String(status || "").toUpperCase();
        if (s === "ACTIVE") return ColorScheme.yellow;
        if (s === "IDLE") return ColorScheme.green;
        if (s === "COOLDOWN" || s === "ERROR") return ColorScheme.red;
        return ColorScheme.textDisabled;
    }

    function getStatusLabel(status, profile) {
        if (!root.isClusterReachable && !root.isSyncing) return "OFFLINE";
        if (profile) {
            if (profile.in_cooldown) return "COOLDOWN";
            if (profile.is_running) return "EM USO";
            if (profile.remaining_tokens === 0 || (profile.five_hour_percent !== undefined && profile.five_hour_percent < 10)) {
                return "ESGOTADO";
            }
        }
        var s = String(status || "").toUpperCase();
        if (s === "ACTIVE") return "EM USO";
        if (s === "IDLE") return "LIVRE";
        if (s === "COOLDOWN") return "COOLDOWN";
        if (s === "ERROR") return "ERRO";
        return "OFFLINE";
    }

    function formatTokens(count) {
        var n = Number(count || 0);
        if (n >= 1000000) return (n / 1000000).toFixed(1) + "M";
        if (n >= 1000) return Math.round(n / 1000) + "k";
        return String(n);
    }

    function formatSteps(count) {
        var n = Number(count || 0);
        if (n >= 1000) return (n / 1000).toFixed(1) + "k";
        return String(n);
    }

    TimedProcess {
        id: statusProc
        timeoutMs: 6000
        timeoutLabel: "AgyHubStatus"
        command: [root.binPath, "status", "--json", "--compact"]
        stdout: StdioCollector {
            onStreamFinished: {
                root.isLoading = false;
                var raw = String(text || "").trim();
                if (!raw) return;
                try {
                    var data = JSON.parse(raw);
                    if (data.profiles && Array.isArray(data.profiles)) {
                        root.profiles = data.profiles;
                    }
                    root.proxyRunning = data.proxy_running === true;
                    root.proxyPort = data.proxy_port || 8317;
                    root.proxyPid = data.proxy_pid || 0;
                    root.proxyRpm = data.proxy_rpm || 0;
                    root.hermesRunning = data.hermes_running === true;
                    root.hermesPid = data.hermes_pid || 0;
                    root.hermesStatus = data.hermes_status || (data.hermes_running ? "ONLINE" : "OFFLINE");
                    root.clusterQuotaTokens = data.cluster_quota_tokens || 12500000;
                    root.clusterRemainingTokens = (data.cluster_remaining_tokens !== undefined) ? data.cluster_remaining_tokens : 12500000;
                    root.clusterQuotaPercent = (data.cluster_quota_percent !== undefined) ? data.cluster_quota_percent : 100;
                    root.clusterFiveHourPercent = (data.cluster_five_hour_percent !== undefined) ? data.cluster_five_hour_percent : 0;
                    root.clusterWeeklyPercent = (data.cluster_weekly_percent !== undefined) ? data.cluster_weekly_percent : 0;
                    root.totalSteps = data.total_steps || 0;
                    root.activeJobs = data.active_jobs || 0;
                    root.resetCountdown = data.reset_countdown || "--";
                    root.lastUpdated = data.last_updated || "";
                    root.lastError = "";
                } catch (e) {
                    root.lastError = e.toString();
                    Logger.warn("AgyHubService", "Erro ao parsear JSON do agy-hub: " + e);
                }
            }
        }
        onExited: (code) => {
            root.isLoading = false;
            if (code !== 0) {
                root.lastError = "Offline (código " + code + ")";
                root.proxyRunning = false;
                root.hermesRunning = false;
                Logger.warn("AgyHubService", "statusProc retornou código: " + code);
            }
        }
        onTimedOut: (label) => {
            root.isLoading = false;
            root.lastError = "Timeout na consulta do cluster";
            root.proxyRunning = false;
            root.hermesRunning = false;
            Logger.warn("AgyHubService", "statusProc timed out");
        }
    }

    TimedProcess {
        id: syncProc
        timeoutMs: 15000
        timeoutLabel: "AgyHubSync"
        command: [root.binPath, "sync"]
        stdout: StdioCollector {
            onStreamFinished: {
                root.isSyncing = false;
                root.showToast("Contas sincronizadas!");
                root.refresh();
            }
        }
        onExited: (code) => {
            root.isSyncing = false;
            if (code !== 0) {
                root.showToast("Falha na sincronização");
            }
        }
        onTimedOut: (label) => {
            root.isSyncing = false;
            root.showToast("Timeout na sincronização");
        }
    }

    TimedProcess {
        id: serviceControlProc
        timeoutMs: 10000
        timeoutLabel: "AgyHubServiceControl"
        onExited: (code) => {
            root.isRestartingProxy = false;
            root.isRestartingHermes = false;
            root.refresh();
        }
        onTimedOut: (label) => {
            root.isRestartingProxy = false;
            root.isRestartingHermes = false;
            root.showToast("Timeout no comando");
        }
    }

    TimedProcess {
        id: tuiProc
        timeoutMs: 0
    }

    TimedProcess {
        id: launchProc
        timeoutMs: 0
    }

    TimedProcess {
        id: restartProc
        timeoutMs: 10000
        timeoutLabel: "AgyHubRestartDaemon"
        onExited: (code) => {
            root.isRestartingDaemon = false;
            root.showToast(code === 0 ? "Hub reiniciado!" : "Falha ao reiniciar hub");
            root.refresh();
        }
        onTimedOut: (label) => {
            root.isRestartingDaemon = false;
            root.showToast("Timeout ao reiniciar hub");
        }
    }

    Timer {
        id: pollTimer
        interval: root.isPopupOpen ? 3500 : 20000
        repeat: true
        running: FeatureFlags.showAgyHub
        onTriggered: root.refresh()
    }

    Timer {
        id: toastTimer
        interval: 3500
        repeat: false
        onTriggered: root.toastMessage = ""
    }

    function showToast(msg) {
        root.toastMessage = msg;
        toastTimer.restart();
    }

    function refresh() {
        if (root.isLoading) return;
        root.isLoading = true;
        statusProc.exec([root.binPath, "status", "--json", "--compact"]);
    }

    function syncAccounts() {
        if (root.isSyncing) return;
        root.isSyncing = true;
        root.showToast("Sincronizando contas...");
        syncProc.exec([root.binPath, "sync"]);
    }

    function restartProxy() {
        if (root.isRestartingProxy) return;
        root.isRestartingProxy = true;
        root.showToast("Reiniciando CLI-Proxy...");
        serviceControlProc.exec(["bash", "-c", "systemctl --user restart cli-proxy-api.service"]);
    }

    function restartHermes() {
        if (root.isRestartingHermes) return;
        root.isRestartingHermes = true;
        root.showToast("Reiniciando Hermes...");
        serviceControlProc.exec(["bash", "-c", "command -v hermes >/dev/null 2>&1 && hermes gateway restart || systemctl --user restart hermes"]);
    }

    function openTui() {
        var userBin = RuntimePaths.joinPath(RuntimePaths.homeDir, ".local/bin");
        tuiProc.exec(["bash", "-c", "export PATH=\"" + userBin + ":${PATH}\"; if command -v foot >/dev/null 2>&1; then foot -T 'agy-hub' agy-hub; elif command -v kitty >/dev/null 2>&1; then kitty -e agy-hub; else xterm -e agy-hub; fi"]);
    }

    function launchProfile(alias) {
        var cmd = String(alias || "agy").trim();
        var userBin = RuntimePaths.joinPath(RuntimePaths.homeDir, ".local/bin");
        root.showToast("Iniciando " + cmd + "...");
        launchProc.exec(["bash", "-c", "export PATH=\"" + userBin + ":${PATH}\"; if command -v foot >/dev/null 2>&1; then foot -T '" + cmd + "' " + cmd + "; elif command -v kitty >/dev/null 2>&1; then kitty -e " + cmd + "; else xterm -e " + cmd + "; fi"]);
    }

    function openConversation(alias, conversationId) {
        var cmd = String(alias || "agy").trim();
        var conv = String(conversationId || "").trim();
        var userBin = RuntimePaths.joinPath(RuntimePaths.homeDir, ".local/bin");
        root.showToast("Retomando sessão...");
        launchProc.exec(["bash", "-c", "export PATH=\"" + userBin + ":${PATH}\"; if command -v foot >/dev/null 2>&1; then foot -T '" + cmd + "-session' " + cmd + " -c " + conv + "; elif command -v kitty >/dev/null 2>&1; then kitty -e " + cmd + " -c " + conv + "; else xterm -e " + cmd + " -c " + conv + "; fi"]);
    }

    function reauthAccount(profileId) {
        var pId = String(profileId || "default").trim();
        var userBin = RuntimePaths.joinPath(RuntimePaths.homeDir, ".local/bin");
        root.showToast("Iniciando login OAuth...");
        launchProc.exec(["bash", "-c", "export PATH=\"" + userBin + ":${PATH}\"; if command -v foot >/dev/null 2>&1; then foot -T 'agy-auth' agy-profile " + pId + " auth; elif command -v kitty >/dev/null 2>&1; then kitty -e agy-profile " + pId + " auth; else xterm -e agy-profile " + pId + " auth; fi"]);
    }

    function copyToClipboard(text) {
        var t = String(text || "").trim();
        if (!t) return;
        var safeT = t.replace(/'/g, "'\\''");
        serviceControlProc.exec(["bash", "-c", "if command -v wl-copy >/dev/null 2>&1; then printf '%s' '" + safeT + "' | wl-copy; elif command -v xclip >/dev/null 2>&1; then printf '%s' '" + safeT + "' | xclip -selection clipboard; fi"]);
        root.showToast("Copiado: " + (t.length > 20 ? (t.substring(0, 17) + "...") : t));
    }

    property int expandedAccountIndex: -1

    function toggleAccountExpanded(index) {
        if (expandedAccountIndex === index) {
            expandedAccountIndex = -1;
        } else {
            expandedAccountIndex = index;
        }
    }

    TimedProcess {
        id: dispatchProc
        timeoutMs: 0
        onExited: (code) => {
            root.isDispatching = false;
            root.refresh();
        }
    }

    function dispatchPrompt(promptText, modelName) {
        var p = String(promptText || "").trim();
        if (!p) return;
        pushPromptHistory(p);
        root.isDispatching = true;
        showToast("Despachando prompt...");
        var args = [root.binPath, "exec", "-b"];
        if (modelName && String(modelName).trim() !== "" && String(modelName).toLowerCase() !== "auto") {
            args.push("-m", String(modelName).trim());
        }
        args.push(p);
        dispatchProc.exec(args);
    }

    function dispatchSwarm(promptText, modelName) {
        var p = String(promptText || "").trim();
        if (!p) return;
        pushPromptHistory(p);
        root.isDispatching = true;
        showToast("Disparando Swarm (5 workers)...");
        var args = [root.binPath, "exec", "-b", "--swarm", "5"];
        if (modelName && String(modelName).trim() !== "" && String(modelName).toLowerCase() !== "auto") {
            args.push("-m", String(modelName).trim());
        }
        args.push(p);
        dispatchProc.exec(args);
    }

    function dispatchPromptToAccount(alias, promptText, modelName) {
        var p = String(promptText || "").trim();
        if (!p) return;
        var a = String(alias || "").trim();
        showToast("Despachando para " + (a || "worker") + "...");
        var args = [root.binPath, "exec", "-b"];
        if (a) {
            args.push("-a", a);
        }
        if (modelName && String(modelName).trim() !== "" && String(modelName).toLowerCase() !== "auto") {
            args.push("-m", String(modelName).trim());
        }
        args.push(p);
        dispatchProc.exec(args);
    }

    function resetCooldown() {
        showToast("Liberando contas...");
        serviceControlProc.exec([root.binPath, "reset-cooldown"]);
    }

    function restartDaemon() {
        root.showToast("Reiniciando daemon...");
        restartProc.exec(["bash", "-c", "systemctl --user restart agy-hub-daemon.service"]);
    }

    Component.onCompleted: {
        refresh();
    }
}
