// dashboard.go — Copilot / local IDE mode only.
//
// RegisterDashboardRoutes mounts a browser UI on a separate port (:8081 default).
// All routes are unauthenticated — intended for localhost-only use.
// Not registered in autonomous-agent server mode (runServer in cmd/main.go).
package api

import (
	"net/http"
	"text/template"
)

var dashboardTmpl = template.Must(template.New("dashboard").Parse(dashboardHTML))

type dashboardData struct {
	APIKey string
}

// RegisterDashboardRoutes mounts the browser dashboard and its data endpoints.
// No authentication — intended for localhost-only use in copilot/mcp-proxy mode.
func (s *APIServer) RegisterDashboardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.serveDashboard)
	mux.Handle("GET /api/sessions", http.HandlerFunc(s.listSessions))
	mux.Handle("GET /api/sessions/{id}/budget", http.HandlerFunc(s.getSessionBudget))
	mux.Handle("GET /api/pipeline", http.HandlerFunc(s.getPipeline))
	mux.Handle("GET /api/stats", http.HandlerFunc(s.getStats))
}

func (s *APIServer) serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardTmpl.Execute(w, dashboardData{APIKey: s.keyStore.Primary()}) //nolint:errcheck
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>PerchGuard Copilot</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d1117;color:#c9d1d9;font-family:'Segoe UI',system-ui,sans-serif;padding:24px;font-size:14px}
h1{font-size:1.2rem;color:#58a6ff;letter-spacing:-.01em}
.meta{font-size:.72rem;color:#484f58;margin-top:3px;margin-bottom:22px}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}
@media(max-width:720px){.grid{grid-template-columns:1fr}}
.card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:16px}
.card h2{font-size:.72rem;text-transform:uppercase;letter-spacing:.06em;color:#8b949e;margin-bottom:12px}
.bar-row{margin-bottom:9px}
.bar-label{display:flex;justify-content:space-between;font-size:.78rem;color:#c9d1d9;margin-bottom:3px}
.bar-label .hint{color:#8b949e}
.bar-track{background:#21262d;border-radius:3px;height:7px}
.bar-fill{height:7px;border-radius:3px;transition:width .35s ease}
.green{background:#3fb950}.yellow{background:#d29922}.red{background:#f85149}
.badges{display:flex;flex-wrap:wrap;gap:5px}
.badge{display:inline-flex;align-items:center;gap:5px;background:#21262d;border:1px solid #30363d;border-radius:4px;padding:3px 9px;font-size:.75rem}
.dot{width:6px;height:6px;border-radius:50%}
.dot.on{background:#3fb950}.dot.off{background:#484f58}
.decision-row{display:flex;justify-content:space-between;font-size:.8rem;padding:4px 0;border-bottom:1px solid #21262d}
.decision-row:last-child{border-bottom:none}
.allow{color:#3fb950}.deny{color:#f85149}.mutate{color:#58a6ff}.terminate{color:#f85149}.human{color:#d29922}
.empty{font-size:.8rem;color:#484f58}
.session-header{font-size:.73rem;color:#8b949e;margin-bottom:7px;margin-top:4px}
.session-header:first-child{margin-top:0}
.enforce-badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:.72rem;margin-bottom:10px}
.enforce-badge.enforce{background:#1a3a1a;color:#3fb950;border:1px solid #2d6a2d}
.enforce-badge.observe{background:#3a3010;color:#d29922;border:1px solid #6a5510}
</style>
</head>
<body>
<h1>PerchGuard Copilot</h1>
<div class="meta" id="ts">Connecting...</div>
<div class="grid">
  <div class="card">
    <h2>Session Budget</h2>
    <div id="budget"><span class="empty">Waiting for sessions...</span></div>
  </div>
  <div class="card">
    <h2>Active Policies</h2>
    <div id="policies"><span class="empty">Loading...</span></div>
  </div>
  <div class="card">
    <h2>Decisions</h2>
    <div id="decisions"><span class="empty">No decisions yet</span></div>
  </div>
  <div class="card">
    <h2>Top Blocked Tools</h2>
    <div id="blocked"><span class="empty">None</span></div>
  </div>
</div>
<script>
const PG_KEY = "{{.APIKey}}";
const PG_HEADERS = PG_KEY ? {'Authorization': 'Bearer ' + PG_KEY} : {};
function pct(used, limit) { return limit > 0 ? Math.min(used / limit, 1) : 0; }
function colorClass(p) { return p >= 0.9 ? 'red' : p >= 0.7 ? 'yellow' : 'green'; }
function fmtNum(n) {
  if (n >= 1000000) return (n/1000000).toFixed(1)+'M';
  if (n >= 1000) return (n/1000).toFixed(1)+'k';
  return String(n);
}
function fmtUSD(v) { return '$' + v.toFixed(2); }

function bar(label, used, limit, fmt) {
  const p = pct(used, limit);
  const cls = colorClass(p);
  return '<div class="bar-row">' +
    '<div class="bar-label"><span>' + label + '</span>' +
    '<span class="hint">' + fmt(used) + ' / ' + fmt(limit) + ' (' + (p*100).toFixed(1) + '%)</span></div>' +
    '<div class="bar-track"><div class="bar-fill ' + cls + '" style="width:' + (p*100).toFixed(2) + '%"></div></div>' +
    '</div>';
}

async function refresh() {
  document.getElementById('ts').textContent =
    'Last refresh: ' + new Date().toLocaleTimeString() + '  •  refreshes every 3s';

  // Budget
  try {
    const sessions = await (await fetch('/api/sessions', {headers: PG_HEADERS})).json();
    if (!Array.isArray(sessions) || sessions.length === 0) {
      document.getElementById('budget').innerHTML = '<span class="empty">No active sessions</span>';
    } else {
      let html = '';
      for (const s of sessions.slice(0, 5)) {
        const sid = s.session_id;
        try {
          const resp = await fetch('/api/sessions/' + sid + '/budget', {headers: PG_HEADERS});
          if (!resp.ok) {
            html += '<div class="session-header">' + sid.substring(0,28) + '</div>' +
                    '<div class="empty" style="margin-bottom:10px">No tool calls yet</div>';
            continue;
          }
          const b = await resp.json();
          const lim = b.limits || {};
          html += '<div class="session-header">' + sid.substring(0,36) +
                  ' &nbsp;&middot;&nbsp; ' + (b.elapsed_minutes||0).toFixed(1) + ' min</div>';
          if ((lim.maxTokensPerSession||0) > 0)
            html += bar('Tokens', b.estimated_tokens||0, lim.maxTokensPerSession, fmtNum);
          if ((lim.maxCostPerSessionUSD||0) > 0)
            html += bar('Cost', b.estimated_cost_usd||0, lim.maxCostPerSessionUSD, fmtUSD);
          if ((lim.maxToolCallsPerSession||0) > 0)
            html += bar('Calls', b.tool_calls||0, lim.maxToolCallsPerSession, fmtNum);
        } catch(_) {
          html += '<div class="empty" style="margin-bottom:8px">' + sid.substring(0,28) + ' — unavailable</div>';
        }
      }
      document.getElementById('budget').innerHTML = html || '<span class="empty">No budget data yet</span>';
    }
  } catch(e) {
    document.getElementById('budget').innerHTML = '<span class="empty">Cannot reach PerchGuard (' + e.message + ')</span>';
  }

  // Pipeline / policies
  try {
    const p = await (await fetch('/api/pipeline', {headers: PG_HEADERS})).json();
    const enforceClass = p.enforcement_mode === 'enforce' ? 'enforce' : 'observe';
    const enforceLabel = p.enforcement_mode === 'enforce' ? '🔒 Enforce' : '👁 Observe';
    let html = '<span class="enforce-badge ' + enforceClass + '">' + enforceLabel + '</span>';
    if (p.policy_hash) {
      html += '<div style="font-size:.72rem;color:#484f58;margin-bottom:8px">hash: ' + p.policy_hash.substring(0,8) + '</div>';
    }
    html += '<div class="badges">';
    const all = [
      ...(p.validators||[]),
      ...(p.mutators||[]),
      ...(p.quotas||[]),
    ];
    html += all.map(name =>
      '<span class="badge"><span class="dot on"></span>' + name + '</span>'
    ).join('');
    html += '</div>';
    document.getElementById('policies').innerHTML = html;
  } catch(_) {
    document.getElementById('policies').innerHTML = '<span class="empty">—</span>';
  }

  // Stats — decisions + blocked tools
  try {
    const st = await (await fetch('/api/stats', {headers: PG_HEADERS})).json();
    const d = st.decisions || {};
    const order = [['ALLOW','allow'],['DENY','deny'],['MUTATE','mutate'],['TERMINATE','terminate'],['HUMAN_REVIEW','human']];
    const rows = order.filter(([k]) => d[k] != null).map(([k, cls]) =>
      '<div class="decision-row"><span class="' + cls + '">' + k + '</span><span>' + d[k] + '</span></div>'
    );
    document.getElementById('decisions').innerHTML =
      rows.length ? rows.join('') : '<span class="empty">No decisions yet</span>';

    const blocked = (st.top_denied_tools||[]);
    document.getElementById('blocked').innerHTML = blocked.length
      ? blocked.map(t =>
          '<div class="decision-row"><span>' + t.tool + '</span><span class="deny">' + t.count + '</span></div>'
        ).join('')
      : '<span class="empty">None</span>';
  } catch(_) {}
}

refresh();
setInterval(refresh, 3000);
</script>
</body>
</html>`
