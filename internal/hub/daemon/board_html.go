package daemon

// BoardHTML define a interface web responsiva autossuficiente (Zero-CDN, Dark Mode, SSE)
const BoardHTML = `<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>AGY-SWARM // Live Cluster & Kanban Board</title>
  <style>
    :root {
      --bg: #0d1117;
      --card-bg: #161b22;
      --border: #30363d;
      --text: #c9d1d9;
      --text-muted: #8b949e;
      --accent: #58a6ff;
      --green: #3fb950;
      --red: #f85149;
      --yellow: #d29922;
      --purple: #bc8cff;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: var(--bg);
      color: var(--text);
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      padding: 24px;
      line-height: 1.5;
    }
    header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding-bottom: 16px;
      border-bottom: 1px solid var(--border);
      margin-bottom: 24px;
    }
    .brand { font-size: 20px; font-weight: 700; letter-spacing: 0.5px; }
    .badge {
      font-size: 12px;
      padding: 4px 10px;
      border-radius: 12px;
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge-live { background: rgba(63, 185, 80, 0.2); color: var(--green); border: 1px solid var(--green); }
    .badge-err { background: rgba(248, 81, 73, 0.2); color: var(--red); border: 1px solid var(--red); }
    .metrics-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }
    .metric-card {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 16px;
    }
    .metric-title { font-size: 12px; color: var(--text-muted); text-transform: uppercase; font-weight: 600; }
    .metric-value { font-size: 28px; font-weight: 700; margin-top: 6px; color: var(--accent); }
    h2 { font-size: 16px; font-weight: 600; margin-bottom: 12px; display: flex; align-items: center; gap: 8px; }
    .table-container {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      overflow-x: auto;
      margin-bottom: 28px;
    }
    table { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
    th { background: #21262d; padding: 10px 14px; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border); }
    td { padding: 10px 14px; border-bottom: 1px solid var(--border); }
    tr:last-child td { border-bottom: none; }
    .progress-bar-bg {
      background: #21262d;
      border-radius: 4px;
      height: 8px;
      width: 100px;
      display: inline-block;
      overflow: hidden;
      vertical-align: middle;
      margin-right: 8px;
    }
    .progress-bar-fill { height: 100%; background: var(--green); transition: width 0.3s; }
    .status-pill {
      display: inline-block;
      padding: 2px 8px;
      border-radius: 10px;
      font-size: 11px;
      font-weight: 600;
    }
    .status-active { background: rgba(63, 185, 80, 0.2); color: var(--green); }
    .status-idle { background: rgba(88, 166, 255, 0.2); color: var(--accent); }
    .status-cooldown { background: rgba(210, 153, 34, 0.2); color: var(--yellow); }
    .status-error { background: rgba(248, 81, 73, 0.2); color: var(--red); }
    .kanban-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
      gap: 16px;
    }
    .kanban-col {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 14px;
      min-height: 200px;
    }
    .kanban-col-header {
      font-size: 13px;
      font-weight: 700;
      color: var(--text-muted);
      margin-bottom: 12px;
      padding-bottom: 8px;
      border-bottom: 1px solid var(--border);
      display: flex;
      justify-content: space-between;
    }
    .task-card {
      background: #21262d;
      border: 1px solid var(--border);
      border-radius: 6px;
      padding: 10px;
      margin-bottom: 10px;
      font-size: 12px;
    }
    .task-id { font-weight: 700; color: var(--accent); margin-bottom: 4px; }
    .task-title { font-weight: 600; margin-bottom: 6px; }
    .task-meta { font-size: 11px; color: var(--text-muted); }
  </style>
</head>
<body>
  <header>
    <div class="brand">⚡ AGY-SWARM // CLUSTER CONTROL PLANE</div>
    <div id="statusBadge" class="badge badge-live">Conectado (SSE)</div>
  </header>

  <div class="metrics-grid">
    <div class="metric-card">
      <div class="metric-title">Tokens Restantes</div>
      <div id="clusterTokens" class="metric-value">--</div>
    </div>
    <div class="metric-card">
      <div class="metric-title">Cota do Cluster</div>
      <div id="clusterQuota" class="metric-value">--%</div>
    </div>
    <div class="metric-card">
      <div class="metric-title">Contas Ativas</div>
      <div id="clusterProfiles" class="metric-value">--</div>
    </div>
    <div class="metric-card">
      <div class="metric-title">RPM Consolidado</div>
      <div id="clusterRPM" class="metric-value">--</div>
    </div>
  </div>

  <h2>📊 Telemetria das 10 Contas Google AI Pro</h2>
  <div class="table-container">
    <table>
      <thead>
        <tr>
          <th>Perfil</th>
          <th>Alias</th>
          <th>E-mail</th>
          <th>Cota Semanal</th>
          <th>Cota 5 Horas</th>
          <th>RPM</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody id="profilesBody">
        <tr><td colspan="7" style="text-align: center; color: var(--text-muted);">Aguardando sincronização com o daemon...</td></tr>
      </tbody>
    </table>
  </div>

  <h2 id="epicTitle">📋 Kanban Swarm // Épico Ativo</h2>
  <div class="kanban-grid">
    <div class="kanban-col">
      <div class="kanban-col-header"><span>⏳ BACKLOG</span><span id="countBacklog">0</span></div>
      <div id="colBacklog"></div>
    </div>
    <div class="kanban-col">
      <div class="kanban-col-header"><span>⚡ IN_PROGRESS</span><span id="countInProgress">0</span></div>
      <div id="colInProgress"></div>
    </div>
    <div class="kanban-col">
      <div class="kanban-col-header"><span>✅ DONE</span><span id="countDone">0</span></div>
      <div id="colDone"></div>
    </div>
    <div class="kanban-col">
      <div class="kanban-col-header"><span>🛑 BLOCKED</span><span id="countBlocked">0</span></div>
      <div id="colBlocked"></div>
    </div>
  </div>

  <script>
    function renderCluster(st) {
      if (!st) return;
      document.getElementById('clusterTokens').textContent = (st.cluster_remaining_tokens || 0).toLocaleString();
      document.getElementById('clusterQuota').textContent = (st.cluster_quota_percent || 0) + '%';
      document.getElementById('clusterProfiles').textContent = (st.healthy_profiles_count || 0) + '/' + (st.profiles ? st.profiles.length : 0);
      
      let totalRPM = 0;
      if (st.profiles) {
        st.profiles.forEach(p => totalRPM += (p.rpm || 0));
        const tbody = document.getElementById('profilesBody');
        tbody.innerHTML = '';
        st.profiles.forEach(p => {
          const tr = document.createElement('tr');
          const weeklyPct = p.weekly_quota ? p.weekly_quota.percent_remaining : 0;
          const fiveHPct = p.five_hour_quota ? p.five_hour_quota.percent_remaining : 0;
          
          let statusClass = 'status-idle';
          if (p.status === 'ACTIVE') statusClass = 'status-active';
          else if (p.in_cooldown || p.status === 'COOLDOWN') statusClass = 'status-cooldown';
          else if (p.status === 'ERROR') statusClass = 'status-error';

          tr.innerHTML = ` + "`" + `
            <td><strong>${p.id}</strong></td>
            <td>${p.alias || '-'}</td>
            <td style="color: var(--text-muted);">${p.email || '-'}</td>
            <td>
              <div class="progress-bar-bg"><div class="progress-bar-fill" style="width: ${weeklyPct}%;"></div></div>
              ${weeklyPct}%
            </td>
            <td>
              <div class="progress-bar-bg"><div class="progress-bar-fill" style="width: ${fiveHPct}%; background: var(--accent);"></div></div>
              ${fiveHPct}%
            </td>
            <td>${p.rpm || 0}</td>
            <td><span class="status-pill ${statusClass}">${p.in_cooldown ? 'COOLDOWN' : (p.status || 'IDLE')}</span></td>
          ` + "`" + `;
          tbody.appendChild(tr);
        });
      }
      document.getElementById('clusterRPM').textContent = totalRPM;
    }

    function renderKanban(board) {
      if (!board) return;
      if (board.epic) {
        document.getElementById('epicTitle').textContent = '📋 Kanban Swarm // Épico: ' + board.epic;
      }
      const cols = {
        'BACKLOG': document.getElementById('colBacklog'),
        'IN_PROGRESS': document.getElementById('colInProgress'),
        'DONE': document.getElementById('colDone'),
        'BLOCKED': document.getElementById('colBlocked')
      };
      const counts = { 'BACKLOG': 0, 'IN_PROGRESS': 0, 'DONE': 0, 'BLOCKED': 0 };
      Object.values(cols).forEach(c => c.innerHTML = '');

      if (board.cards) {
        board.cards.forEach(c => {
          const st = (c.status || 'BACKLOG').toUpperCase();
          if (cols[st]) {
            counts[st]++;
            const div = document.createElement('div');
            div.className = 'task-card';
            let meta = '';
            if (c.winner) meta += ` + "`" + `🏆 ${c.winner}<br>` + "`" + `;
            if (c.error) meta += ` + "`" + `<span style="color: var(--red);">${c.error}</span><br>` + "`" + `;
            if (c.depends_on && c.depends_on.length > 0) meta += ` + "`" + `🔗 Dep: ${c.depends_on.join(', ')}<br>` + "`" + `;
            div.innerHTML = ` + "`" + `
              <div class="task-id">${c.id}</div>
              <div class="task-title">${c.title}</div>
              <div class="task-meta">${meta}</div>
            ` + "`" + `;
            cols[st].appendChild(div);
          }
        });
      }
      document.getElementById('countBacklog').textContent = counts['BACKLOG'];
      document.getElementById('countInProgress').textContent = counts['IN_PROGRESS'];
      document.getElementById('countDone').textContent = counts['DONE'];
      document.getElementById('countBlocked').textContent = counts['BLOCKED'];
    }

    function fetchKanban() {
      fetch('/api/kanban')
        .then(r => r.json())
        .then(renderKanban)
        .catch(() => {});
    }

    // Server-Sent Events setup
    function connectSSE() {
      const evtSource = new EventSource('/events');
      evtSource.onmessage = function(e) {
        try {
          const data = JSON.parse(e.data);
          renderCluster(data);
          document.getElementById('statusBadge').className = 'badge badge-live';
          document.getElementById('statusBadge').textContent = 'Conectado (SSE)';
        } catch(err) {}
      };
      evtSource.onerror = function() {
        document.getElementById('statusBadge').className = 'badge badge-err';
        document.getElementById('statusBadge').textContent = 'Desconectado (Reconectando...)';
      };
    }

    connectSSE();
    fetch('/api/status').then(r => r.json()).then(renderCluster).catch(() => {});
    fetchKanban();
    setInterval(fetchKanban, 3000);
  </script>
</body>
</html>`
