/**
 * wecom-komari Cloudflare Worker
 * 
 * Endpoints:
 *   GET  /webhook          - Health check
 *   POST /webhook          - Forward message to Telegram + WeCom
 *   POST /telegram/push    - Direct Telegram push API
 *   POST /telegram/webhook - Telegram Bot webhook
 *   POST /wecomchan        - WeCom message sending
 *   GET  /healthz          - Health check
 */

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;

    // CORS preflight
    if (request.method === 'OPTIONS') {
      return new Response(null, {
        headers: {
          'Access-Control-Allow-Origin': '*',
          'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
          'Access-Control-Allow-Headers': 'Content-Type',
        },
      });
    }

    try {
      if (path === '/webhook' || path === '/') {
        return handleWebhook(request, env);
      }
      if (path === '/telegram/push') {
        return handleTelegramPush(request, env);
      }
      if (path === '/telegram/webhook') {
        return handleTelegramWebhook(request, env);
      }
      if (path === '/wecomchan') {
        return handleWeComChan(request, env);
      }
      if (path === '/healthz' || path === '/readyz') {
        return jsonResponse({ status: 'ok' });
      }

      return jsonResponse({ error: 'not found' }, 404);
    } catch (err) {
      console.error('[Worker]', err);
      return jsonResponse({ error: err.message }, 500);
    }
  },
};

// ─── Helpers ───────────────────────────────────────────────

function checkAuth(request, env, body = {}) {
  const url = new URL(request.url);
  const key = body.sendkey || body.token || url.searchParams.get('sendkey') || url.searchParams.get('token');
  return key === env.SENDKEY;
}

function isUserAllowed(env, userId) {
  if (!env.TELEGRAM_ALLOWED_USERS) return true;
  const allowed = env.TELEGRAM_ALLOWED_USERS.split(',').map(id => id.trim());
  return allowed.includes(String(userId));
}
/**
 * Komari API helper - supports both Bearer token and session auth
 */
let _komariSessionToken = null;

async function komariLogin(env) {
  if (_komariSessionToken) return _komariSessionToken;
  if (!env.KOMARI_USERNAME || !env.KOMARI_PASSWORD) return null;
  try {
    const resp = await fetch(env.KOMARI_URL + '/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: env.KOMARI_USERNAME, password: env.KOMARI_PASSWORD }),
    });
    const data = await resp.json().catch(() => ({}));
    if (data.status === 'success' && data.data?.set_cookie?.session_token) {
      _komariSessionToken = data.data.set_cookie.session_token;
      return _komariSessionToken;
    }
    return null;
  } catch { return null; }
}

async function komariReqHeaders(env) {
  // Try Bearer token first
  if (env.KOMARI_API_KEY) {
    return { 'Authorization': 'Bearer ' + env.KOMARI_API_KEY };
  }
  // Fallback to session auth
  const token = await komariLogin(env);
  if (token) {
    return { 'Cookie': 'session_token=' + token };
  }
  return {};
}

async function komariReq(env, path) {
  if (!env.KOMARI_URL) return null;
  try {
    const headers = await komariReqHeaders(env);
    const resp = await fetch(env.KOMARI_URL + path, { headers });
    const data = await resp.json().catch(() => ({}));
    if (data.status !== 'success') return null;
    return data.data;
  } catch { return null; }
}

async function komariAdminReq(env, method, path, body) {
  if (!env.KOMARI_URL) return null;
  try {
    const headers = await komariReqHeaders(env);
    headers['Content-Type'] = 'application/json';
    const opts = { method, headers };
    if (body) opts.body = JSON.stringify(body);
    const resp = await fetch(env.KOMARI_URL + path, opts);
    const text = await resp.text();
    let data;
    try { data = JSON.parse(text); } catch { return null; }
    // Handle wrapped response: {"status":"success","data":...}
    if (data.status === 'success') return data.data;
    // Handle wrapped error
    if (data.status && data.status !== 'success') {
      console.error('[komariAdminReq]', path, data.status, data.message);
      return null;
    }
    // Handle raw response (array or object) — return as-is
    if (Array.isArray(data) || (typeof data === 'object' && data !== null)) return data;
    return null;
  } catch (err) {
    console.error('[komariAdminReq]', path, err.message);
    return null;
  }
}

async function getNodeListAndRT(env) {
  const nodes = await komariReq(env, '/api/nodes');
  if (!nodes) return { nodes: null, rtMap: null };

  // Fetch realtime data per node (like Go version: /api/recent/{uuid})
  const rtMap = {};
  const visible = nodes.filter(n => !n.hidden);
  const results = await Promise.allSettled(
    visible.map(async (n) => {
      const data = await komariReq(env, '/api/recent/' + n.uuid);
      if (data && Array.isArray(data) && data.length > 0) {
        rtMap[n.uuid] = data[data.length - 1]; // last entry = latest
      }
    })
  );

  return { nodes, rtMap };
}

// ─── Formatters ────────────────────────────────────────────

function fmtBytes(b) {
  if (b >= 1e12) return (b / 1e12).toFixed(2) + ' TB';
  if (b >= 1e9) return (b / 1e9).toFixed(2) + ' GB';
  if (b >= 1e6) return (b / 1e6).toFixed(2) + ' MB';
  if (b >= 1e3) return (b / 1e3).toFixed(2) + ' KB';
  return b + ' B';
}

function fmtDur(s) {
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}天${h}小时${m}分`;
  if (h > 0) return `${h}小时${m}分`;
  return `${m}分钟`;
}

function fmtSpeed(b) {
  if (b >= 1024 * 1024) return (b / (1024 * 1024)).toFixed(2) + ' MB/s';
  if (b >= 1024) return (b / 1024).toFixed(2) + ' KB/s';
  return Math.round(b) + ' B/s';
}

function fmtNodeStatus(n, rt) {
  let s = `*${n.name}*`;
  if (n.region) s += ' ' + n.region;
  s += '\n';
  if (!rt) {
    s += '🔴 离线\n';
    s += `📋 ${n.os || ''} | ${n.arch || ''}\n`;
    s += `💾 内存: ${fmtBytes(n.mem_total)}\n`;
    s += `📁 磁盘: ${fmtBytes(n.disk_total)}`;
    return s;
  }
  s += '🟢 在线\n';
  s += `📋 ${n.os || ''} | ${n.arch || ''}\n`;
  if (n.virtualization) s += `🔧 ${n.virtualization}\n`;
  s += `🔲 CPU: ${n.cpu_name || ''} (${n.cpu_cores || 0}核) - ${(rt.cpu?.usage || 0).toFixed(1)}%\n`;
  const memPct = n.mem_total > 0 ? (rt.ram?.used / n.mem_total * 100) : 0;
  s += `💾 内存: ${fmtBytes(rt.ram?.used || 0)} / ${fmtBytes(n.mem_total)} (${memPct.toFixed(1)}%)\n`;
  if (n.swap_total > 0) {
    const sp = (rt.swap?.used / n.swap_total * 100);
    s += `💿 Swap: ${fmtBytes(rt.swap?.used || 0)} / ${fmtBytes(n.swap_total)} (${sp.toFixed(1)}%)\n`;
  }
  const dPct = n.disk_total > 0 ? (rt.disk?.used / n.disk_total * 100) : 0;
  s += `📁 磁盘: ${fmtBytes(rt.disk?.used || 0)} / ${fmtBytes(n.disk_total)} (${dPct.toFixed(1)}%)\n`;
  s += `🌐 网络: ↑${fmtSpeed(rt.network?.up || 0)} ↓${fmtSpeed(rt.network?.down || 0)}\n`;
  s += `📊 总流量: ↑${fmtBytes(rt.network?.total_up || 0)} ↓${fmtBytes(rt.network?.total_down || 0)}\n`;
  s += `📈 负载: ${(rt.load?.load1 || 0).toFixed(2)} / ${(rt.load?.load5 || 0).toFixed(2)} / ${(rt.load?.load15 || 0).toFixed(2)}\n`;
  s += `🔗 TCP ${rt.connections?.tcp || 0} | UDP ${rt.connections?.udp || 0}\n`;
  s += `⚙️ 进程: ${rt.process || 0}\n`;
  s += `⏰ 运行: ${fmtDur(rt.uptime || 0)}`;
  if (n.group) s += `\n📂 ${n.group}`;
  if (n.tags) s += `\n🏷 ${n.tags}`;
  if (n.price > 0) s += `\n💰 ${n.price}${n.currency || ''}/${n.billing_cycle || 30}天`;
  if (n.expired_at && !n.expired_at.startsWith('0001')) s += `\n📅 到期: ${n.expired_at.slice(0, 10)}`;
  if (n.public_remark) s += `\n📝 备注: ${n.public_remark}`;
  if (n.remark) s += `\n🔒 私有备注: ${n.remark}`;
  return s;
}

function fmtNodeList(nodes, rtMap) {
  if (!nodes || nodes.length === 0) return '暂无节点';
  let txt = `共 ${nodes.length} 个节点:\n\n`;
  for (let i = 0; i < nodes.length; i++) {
    const n = nodes[i];
    const emoji = n.hidden ? '🙈' : (rtMap[n.uuid] ? '🟢' : '🔴');
    txt += `${i + 1}. ${emoji} ${n.name}`;
    if (n.region) txt += ` ${n.region}`;
    if (n.group) txt += ` [${n.group}]`;
    txt += '\n';
  }
  return txt;
}

function fmtOfflineNodes(nodes, rtMap) {
  const offline = nodes.filter(n => !n.hidden && !rtMap[n.uuid]);
  if (offline.length === 0) return '✅ 所有节点在线';
  let txt = `🔴 离线节点 (${offline.length}):\n\n`;
  for (const n of offline) {
    txt += `🔴 ${n.name}`;
    if (n.region) txt += ` ${n.region}`;
    txt += '\n';
  }
  return txt;
}

function fmtCPUUsageRank(nodes, rtMap) {
  const online = nodes.filter(n => !n.hidden && rtMap[n.uuid]);
  if (online.length === 0) return '暂无在线节点';
  const ranked = online.map(n => ({
    name: n.name,
    cpu: rtMap[n.uuid]?.cpu?.usage || 0,
  })).sort((a, b) => b.cpu - a.cpu);
  let txt = '📊 CPU 使用排名:\n';
  for (let i = 0; i < Math.min(ranked.length, 10); i++) {
    txt += `${i + 1}. ${ranked[i].name} - ${ranked[i].cpu.toFixed(1)}%\n`;
  }
  return txt;
}

function fmtMemUsageRank(nodes, rtMap) {
  const online = nodes.filter(n => !n.hidden && rtMap[n.uuid] && n.mem_total > 0);
  if (online.length === 0) return '暂无在线节点';
  const ranked = online.map(n => ({
    name: n.name,
    pct: rtMap[n.uuid]?.ram?.used / n.mem_total * 100,
  })).sort((a, b) => b.pct - a.pct);
  let txt = '💾 内存使用排名:\n';
  for (let i = 0; i < Math.min(ranked.length, 10); i++) {
    txt += `${i + 1}. ${ranked[i].name} - ${ranked[i].pct.toFixed(1)}%\n`;
  }
  return txt;
}

function fmtNetUsageRank(nodes, rtMap) {
  const online = nodes.filter(n => !n.hidden && rtMap[n.uuid]);
  if (online.length === 0) return '暂无在线节点';
  const ranked = online.map(n => ({
    name: n.name,
    up: rtMap[n.uuid]?.network?.up || 0,
    down: rtMap[n.uuid]?.network?.down || 0,
  })).sort((a, b) => (b.up + b.down) - (a.up + a.down));
  let txt = '🌐 网络使用排名:\n';
  for (let i = 0; i < Math.min(ranked.length, 10); i++) {
    txt += `${i + 1}. ${ranked[i].name} - ↑${fmtSpeed(ranked[i].up)} ↓${fmtSpeed(ranked[i].down)}\n`;
  }
  return txt;
}

function fmtGroupList(nodes) {
  const groups = {};
  for (const n of nodes) {
    const g = n.group || '未分组';
    if (!groups[g]) groups[g] = [];
    groups[g].push(n);
  }
  let txt = '📂 节点分组:\n\n';
  for (const [g, list] of Object.entries(groups)) {
    txt += `📁 ${g} (${list.length}个节点)\n`;
  }
  return txt;
}

function fmtPingInfo(pingData) {
  if (!pingData || !pingData.length) return '📡 暂无Ping任务';
  let txt = `📡 Ping 任务 (${pingData.length}):\n\n`;
  for (const p of pingData) {
    const status = p.enabled ? '✅' : '⏸';
    txt += `${status} ${p.name || p.target || '未命名'}: ${p.target}\n`;
    if (p.interval) txt += `   间隔: ${p.interval}秒\n`;
  }
  return txt;
}

// ─── Komari Command Handlers ───────────────────────────────

async function cmdStatus(env) {
  const { nodes, rtMap } = await getNodeListAndRT(env);
  if (!nodes) return '❌ 无法获取节点数据';

  let total = 0, online = 0, offline = 0;
  let totalCPU = 0, totalMemUsed = 0, totalMemTotal = 0;
  for (const n of nodes) {
    if (n.hidden) continue;
    total++;
    if (rtMap[n.uuid]) {
      online++;
      totalCPU += rtMap[n.uuid].cpu?.usage || 0;
      totalMemUsed += rtMap[n.uuid].ram?.used || 0;
      totalMemTotal += n.mem_total || 0;
    } else {
      offline++;
    }
  }
  const avgCPU = online > 0 ? totalCPU / online : 0;
  const avgMem = totalMemTotal > 0 ? totalMemUsed / totalMemTotal * 100 : 0;

  return `📊 *节点状态概览*\n\n` +
    `📋 总数: ${total}\n` +
    `🟢 在线: ${online}\n` +
    `🔴 离线: ${offline}\n\n` +
    `🔲 平均CPU: ${avgCPU.toFixed(1)}%\n` +
    `💾 平均内存: ${avgMem.toFixed(1)}%`;
}

async function cmdList(env) {
  const { nodes, rtMap } = await getNodeListAndRT(env);
  if (!nodes) return '❌ 无法获取节点列表';
  return fmtNodeList(nodes, rtMap || {});
}

async function cmdOffline(env) {
  const { nodes, rtMap } = await getNodeListAndRT(env);
  if (!nodes) return '❌ 无法获取节点列表';
  return fmtOfflineNodes(nodes, rtMap || {});
}

async function cmdRank(env, type) {
  const { nodes, rtMap } = await getNodeListAndRT(env);
  if (!nodes) return '❌ 无法获取节点列表';
  if (!rtMap) return '❌ 无法获取实时数据';
  switch (type) {
    case 'mem': return fmtMemUsageRank(nodes, rtMap);
    case 'net': return fmtNetUsageRank(nodes, rtMap);
    default: return fmtCPUUsageRank(nodes, rtMap);
  }
}

async function cmdInfo(env) {
  const info = await komariReq(env, '/api/site');
  if (!info) return '❌ 无法获取站点信息';
  let txt = '📋 站点信息\n\n';
  if (info.name) txt += `名称: ${info.name}\n`;
  if (info.description) txt += `描述: ${info.description}\n`;
  if (info.url) txt += `地址: ${info.url}\n`;

  const { nodes, rtMap } = await getNodeListAndRT(env);
  if (nodes) {
    const onlineCount = nodes.filter(n => !n.hidden && rtMap && rtMap[n.uuid]).length;
    txt += `\n节点: ${onlineCount}/${nodes.length} 在线`;
  }
  return txt;
}

async function cmdGroup(env) {
  const { nodes } = await getNodeListAndRT(env);
  if (!nodes) return '❌ 无法获取节点列表';
  return fmtGroupList(nodes);
}

async function cmdPing(env) {
  const data = await komariReq(env, '/api/ping');
  return fmtPingInfo(data);
}

async function cmdNode(env, uuid) {
  const nodes = await komariReq(env, '/api/nodes');
  if (!nodes) return null;
  const n = nodes.find(x => x.uuid === uuid);
  if (!n) return null;
  // Fetch realtime data from /api/recent/{uuid}
  let rtData = null;
  const data = await komariReq(env, '/api/recent/' + uuid);
  if (data && Array.isArray(data) && data.length > 0) {
    rtData = data[data.length - 1];
  }
  return { text: fmtNodeStatus(n, rtData), node: n };
}

async function cmdHistory(env, uuid) {
  const nodes = await komariReq(env, '/api/nodes');
  if (!nodes) return '❌ 无法获取节点信息';
  const n = nodes.find(x => x.uuid === uuid);
  if (!n) return '❌ 节点不存在';

  const recs = await komariReq(env, `/api/records/load?uuid=${uuid}&hours=1`);
  if (!recs || recs.length === 0) return `📊 ${n.name} - 暂无历史记录`;

  let avgCPU = 0, avgMem = 0, avgDisk = 0;
  for (const r of recs) {
    avgCPU += r.cpu || 0;
    avgMem += r.ram || 0;
    avgDisk += r.disk || 0;
  }
  const cnt = recs.length;
  return `📊 *${n.name} - 最近1小时*\n\n` +
    `🔲 平均CPU: ${(avgCPU / cnt).toFixed(1)}%\n` +
    `💾 平均内存: ${fmtBytes(Math.round(avgMem / cnt))}\n` +
    `📁 平均磁盘: ${fmtBytes(Math.round(avgDisk / cnt))}\n` +
    `📈 记录数: ${cnt}`;
}

async function cmdAdmin(env) {
  return '🔧 *管理员面板*\n\n选择操作:';
}

async function cmdAdminClients(env) {
  const data = await komariAdminReq(env, 'GET', '/api/admin/client/list');
  if (!data) return '❌ 无法获取客户端列表\n\n可能原因：\n• API Key 无管理权限\n• Komari 需要用户名/密码认证\n• 端点路径不正确';
  const clients = Array.isArray(data) ? data : [];
  if (clients.length === 0) return '暂无客户端';
  let txt = `📋 客户端列表 (${clients.length}):\n\n`;
  for (let i = 0; i < clients.length; i++) {
    const c = clients[i];
    txt += `${i + 1}. ${c.name || '未命名'}`;
    if (c.remark) txt += ` (${c.remark})`;
    txt += '\n';
  }
  return txt;
}

async function cmdAdminClientDetail(env, uuid) {
  const data = await komariAdminReq(env, 'GET', `/api/admin/client/${uuid}`);
  if (!data) return '❌ 无法获取客户端详情';
  const c = Array.isArray(data) ? data[0] : data;
  if (!c) return '❌ 客户端不存在';
  let txt = `📋 *客户端详情*\n\n`;
  txt += `名称: ${c.name || '未命名'}\n`;
  txt += `UUID: \`${c.uuid}\`\n`;
  if (c.remark) txt += `备注: ${c.remark}\n`;
  if (c.group) txt += `分组: ${c.group}\n`;
  if (c.region) txt += `地区: ${c.region}\n`;
  if (c.os) txt += `系统: ${c.os}\n`;
  if (c.arch) txt += `架构: ${c.arch}\n`;
  if (c.token) txt += `Token: \`${c.token}\`\n`;
  return txt;
}

async function cmdAdminClientToken(env, uuid) {
  const data = await komariAdminReq(env, 'GET', `/api/admin/client/${uuid}`);
  if (!data) return '❌ 无法获取Token';
  const c = Array.isArray(data) ? data[0] : data;
  if (!c) return '❌ 客户端不存在';
  const token = c.token;
  if (!token) return '❌ Token不存在 (Komari版本可能不支持Token API)';
  const siteURL = (env.KOMARI_URL || '').replace(/\/$/, '');
  const linuxCmd = `wget -qO- https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.sh | sudo bash -s -- -e ${siteURL} -t ${token}`;
  const winCmd = `powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "iwr 'https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.ps1' -UseBasicParsing -OutFile 'install.ps1'; & '.\\install.ps1' '-e' '${siteURL}' '-t' '${token}'"`;
  return `🔑 *Token*\n\n\`${token}\`\n\n` +
    `📦 *Linux 一键安装:*\n\`\`\`\n${linuxCmd}\n\`\`\`\n\n` +
    `📦 *Windows PowerShell:*\n\`\`\`\n${winCmd}\n\`\`\`\n\n` +
    `📦 *macOS / FreeBSD:*\n手动下载: [GitHub Releases](https://github.com/komari-monitor/komari-agent/releases)`;
}

async function cmdAdminNotify(env) {
  const offline = await komariAdminReq(env, 'GET', '/api/admin/notification/offline');
  const traffic = await komariAdminReq(env, 'GET', '/api/admin/notification/traffic-report/');
  let txt = '🔔 *通知管理*\n\n';
  txt += `离线通知: ${offline && offline.enabled ? '✅ 开启' : '⏸ 关闭'}\n`;
  txt += `流量报告: ${traffic && traffic.enabled ? '✅ 开启' : '⏸ 关闭'}\n`;
  return txt;
}

async function cmdAdminSettings(env) {
  const settings = await komariAdminReq(env, 'GET', '/api/admin/settings/');
  if (!settings) return '❌ 无法获取设置';
  let txt = '⚙️ *Komari 设置*\n\n';
  const keys = ['site_name', 'site_description', 'site_url', 'language', 'theme'];
  for (const k of keys) {
    if (settings[k] !== undefined) {
      txt += `${k}: ${settings[k]}\n`;
    }
  }
  return txt;
}

async function cmdAdminLogs(env, page) {
  const data = await komariAdminReq(env, 'GET', `/api/admin/logs?limit=10&page=${page || 1}`);
  if (!data) return '❌ 无法获取日志';
  const logs = Array.isArray(data) ? data : data.logs || data.items || [];
  if (logs.length === 0) return '📜 暂无日志';
  let txt = `📜 *审计日志* (第${page || 1}页)\n\n`;
  for (const l of logs.slice(0, 10)) {
    const time = l.created_at ? new Date(l.created_at).toLocaleString('zh-CN') : '';
    txt += `${time} ${l.action || l.type || ''}: ${l.detail || l.message || ''}\n`;
  }
  return txt;
}

async function cmdAdminTasks(env) {
  const data = await komariAdminReq(env, 'GET', '/api/admin/task/all');
  if (!data) return '❌ 无法获取任务列表';
  const tasks = Array.isArray(data) ? data : data.tasks || data.items || [];
  if (tasks.length === 0) return '📋 暂无远程任务';
  let txt = `📋 *远程任务* (${tasks.length})\n\n`;
  for (const t of tasks.slice(0, 10)) {
    txt += `• ${t.name || t.command || '未命名'}`;
    if (t.status) txt += ` [${t.status}]`;
    txt += '\n';
  }
  return txt;
}

async function cmdAdminPingTasks(env) {
  const data = await komariAdminReq(env, 'GET', '/api/admin/ping/');
  if (!data) return '❌ 无法获取Ping任务';
  const tasks = Array.isArray(data) ? data : data.tasks || data.items || [];
  if (tasks.length === 0) return '📡 暂无Ping任务';
  let txt = `📡 *Ping 任务* (${tasks.length})\n\n`;
  for (const t of tasks) {
    const status = t.enabled ? '✅' : '⏸';
    txt += `${status} ${t.name || t.target || '未命名'}: ${t.target}\n`;
  }
  return txt;
}

/**
 * Handle /edit command: /edit uuid_prefix field=value [field2=value2]
 * Supported fields: name, group, region, public_remark
 */
async function handleEditCommand(env, chatId, args) {
  if (!args) {
    await sendTelegram(env, chatId, 'Usage: `/edit uuid_prefix field=value`\n\nExample:\n`/edit 26f3078e name=NewName`\n`/edit 26f3078e group=home region=CN`\n\nAvailable fields: name, group, region, public_remark');
    return;
  }

  const parts = args.trim().split(/\s+/);
  if (parts.length < 2) {
    await sendTelegram(env, chatId, `❌ 字段不足\n\n用法: \`/edit uuid_prefix field=value\``);
    return;
  }

  const uuidPrefix = parts[0];
  const params = {};
  for (let i = 1; i < parts.length; i++) {
    const eqIdx = parts[i].indexOf('=');
    if (eqIdx <= 0) {
      await sendTelegram(env, chatId, `❌ 格式错误: ${parts[i]}\n\n应为 field=value 格式`);
      return;
    }
    const key = parts[i].slice(0, eqIdx);
    const val = parts[i].slice(eqIdx + 1);
    if (!['name', 'group', 'region', 'public_remark'].includes(key)) {
      await sendTelegram(env, chatId, `❌ 不支持的字段: ${key}\n\n可用字段 (English): name, group, region, public_remark`);
      return;
    }
    params[key] = val;
  }

  // Find full UUID by prefix
  const nodes = await komariReq(env, '/api/nodes');
  if (!nodes) {
    await sendTelegram(env, chatId, '❌ 无法获取节点列表');
    return;
  }
  const node = nodes.find(n => n.uuid.startsWith(uuidPrefix));
  if (!node) {
    await sendTelegram(env, chatId, `❌ 未找到 UUID 以 ${uuidPrefix} 开头的节点`);
    return;
  }

  // Submit edit
  const result = await komariAdminReq(env, 'POST', `/api/admin/client/${node.uuid}/edit`, params);
  if (result !== null) {
    let txt = `✅ *${node.name}* 已更新\n\n`;
    for (const [k, v] of Object.entries(params)) {
      txt += `• ${k}: ${v}\n`;
    }
    await sendTelegramKB(env, chatId, txt, [
      [{ text: '📋 查看详情', callback_data: `adm_cd:${node.uuid}` }],
    ]);
  } else {
    await sendTelegram(env, chatId, '❌ 修改失败，请检查参数');
  }
}

async function cmdAdminSessions(env) {
  const data = await komariAdminReq(env, 'GET', '/api/admin/session/get');
  if (!data) return '❌ 无法获取会话';
  const sessions = Array.isArray(data) ? data : data.sessions || data.items || [];
  if (sessions.length === 0) return '🔐 暂无活跃会话';
  let txt = `🔐 *活跃会话* (${sessions.length})\n\n`;
  for (const s of sessions.slice(0, 10)) {
    txt += `• ${s.user_agent || '未知设备'}\n`;
    if (s.ip) txt += `  IP: ${s.ip}\n`;
    if (s.created_at) txt += `  时间: ${new Date(s.created_at).toLocaleString('zh-CN')}\n`;
  }
  return txt;
}

// ─── HTTP Handlers ─────────────────────────────────────────

async function handleWebhook(request, env) {
  if (request.method === 'GET') {
    return jsonResponse({ status: 'ok', service: 'wecom-komari-cf' });
  }
  if (request.method !== 'POST') {
    return jsonResponse({ error: 'method not allowed' }, 405);
  }

  const body = await request.json().catch(() => ({}));
  if (!checkAuth(request, env, body)) {
    return jsonResponse({ error: 'unauthorized' }, 401);
  }

  const msg = body.text || body.msg || body.content;
  if (!msg) {
    return jsonResponse({ error: 'missing text/msg/content' }, 400);
  }

  let sent = false;

  if (env.TELEGRAM_BOT_TOKEN && env.TELEGRAM_ALLOWED_USERS) {
    const chatIds = env.TELEGRAM_ALLOWED_USERS.split(',').map(id => id.trim()).filter(Boolean);
    for (const chatId of chatIds) {
      const result = await sendTelegram(env, chatId, msg);
      if (result.ok) sent = true;
    }
  }

  if (env.WECOM_CID && env.WECOM_SECRET) {
    const result = await sendWeCom(env, env.WECOM_TOUID || '@all', env.WECOM_AID || '', msg);
    if (result.ok) sent = true;
  }

  return jsonResponse({ status: 'ok', sent });
}

async function handleTelegramPush(request, env) {
  if (request.method !== 'POST') return jsonResponse({ error: 'method not allowed' }, 405);
  const body = await request.json().catch(() => ({}));
  if (!checkAuth(request, env, body)) return jsonResponse({ error: 'unauthorized' }, 401);
  if (!body.chat_id || !body.text) return jsonResponse({ error: 'missing chat_id or text' }, 400);
  const result = await sendTelegram(env, body.chat_id, body.text);
  return jsonResponse({ status: result.ok ? 'ok' : 'fail' });
}

async function handleWeComChan(request, env) {
  if (request.method !== 'POST') return jsonResponse({ error: 'method not allowed' }, 405);
  const body = await request.json().catch(() => ({}));
  if (!checkAuth(request, env, body)) return jsonResponse({ error: 'unauthorized' }, 401);
  const msg = body.msg || body.content;
  if (!msg) return jsonResponse({ error: 'missing msg/content' }, 400);
  const toUser = body.touser || env.WECOM_TOUID || '@all';
  const agentId = body.agentid || env.WECOM_AID || '';
  const result = await sendWeCom(env, toUser, agentId, msg);
  return jsonResponse({ status: result.ok ? 'ok' : 'fail' });
}

// ─── Telegram Bot ──────────────────────────────────────────

async function handleTelegramWebhook(request, env) {
  if (request.method !== 'POST') return jsonResponse({ error: 'method not allowed' }, 405);

  if (env.TELEGRAM_WEBHOOK_SECRET) {
    const secret = request.headers.get('X-Telegram-Bot-Api-Secret-Token');
    if (secret !== env.TELEGRAM_WEBHOOK_SECRET) {
      return jsonResponse({ error: 'forbidden' }, 403);
    }
  }

  const update = await request.json().catch(() => ({}));

  if (update.callback_query) {
    const cb = update.callback_query;
    const answerText = cb.data.startsWith('adm_ce:') ? '✏️ 正在加载编辑表单...' : '';
    await answerCallback(env, cb.id, answerText);
    if (!isUserAllowed(env, cb.from.id)) return jsonResponse({ status: 'ok' });
    const chatId = cb.message ? cb.message.chat.id : cb.from.id;
    const msgId = cb.message ? cb.message.message_id : null;
    await handleCallbackData(env, chatId, msgId, cb.data);
    return jsonResponse({ status: 'ok' });
  }

  if (update.message && update.message.text) {
    await handleMessage(env, update.message);
  }

  return jsonResponse({ status: 'ok' });
}

const HELP_TEXT =
  '*wecom-komari Bot*\n\n' +
  '*查询命令:*\n' +
  '/status - 服务器状态概览\n' +
  '/list - 所有节点列表\n' +
  '/offline - 离线节点\n' +
  '/ping - Ping任务信息\n' +
  '/rank - 资源使用排名\n' +
  '/info - 站点信息\n' +
  '/group - 节点分组\n\n' +
  '*管理命令:*\n' +
  '/admin - 管理员面板\n' +
  '/edit uuid_prefix name=Name - Edit client\n' +
  '/notify - 通知管理\n' +
  '/ping_admin - Ping任务管理\n' +
  '/task - 远程任务\n' +
  '/logs - 审计日志\n' +
  '/settings - 设置\n' +
  '/sessions - 活跃会话\n\n' +
  '直接输入节点名称查看详情';

const HELP_BUTTONS = [
  [{ text: '📊 状态', callback_data: 'cmd:status' }, { text: '📋 列表', callback_data: 'cmd:list' }],
  [{ text: '🔴 离线', callback_data: 'cmd:offline' }, { text: '📡 Ping', callback_data: 'cmd:ping' }],
  [{ text: '🏆 排名', callback_data: 'cmd:rank' }, { text: 'ℹ️ 信息', callback_data: 'cmd:info' }],
  [{ text: '📂 分组', callback_data: 'cmd:group' }, { text: '🔧 管理', callback_data: 'adm' }],
  [{ text: '⚙️ 设置', callback_data: 'adm_set' }],
];

async function handleMessage(env, message) {
  const chatId = message.chat.id;
  const text = message.text.trim();
  const userId = message.from.id;

  if (!isUserAllowed(env, userId)) {
    await sendTelegram(env, chatId, '⚠️ 无权限');
    return;
  }

  // Handle commands
  if (text.startsWith('/')) {
    const parts = text.slice(1).split(' ');
    const cmd = parts[0].split('@')[0].toLowerCase();
    const args = parts.slice(1).join(' ');

    switch (cmd) {
      case 'start':
      case 'help':
        await sendTelegramKB(env, chatId, HELP_TEXT, HELP_BUTTONS);
        break;

      case 'status':
        await sendTelegramKB(env, chatId, await cmdStatus(env), [
          [{ text: '🔄 刷新', callback_data: 'cmd:status' }, { text: '📋 列表', callback_data: 'cmd:list' }],
          [{ text: '🔴 离线', callback_data: 'cmd:offline' }, { text: '🏆 排名', callback_data: 'cmd:rank' }],
        ]);
        break;

      case 'list':
        await cmdListWithButtons(env, chatId);
        break;

      case 'offline':
        await cmdOfflineWithButtons(env, chatId);
        break;

      case 'ping':
        await sendTelegramKB(env, chatId, await cmdPing(env), [
          [{ text: '🔄 刷新', callback_data: 'cmd:ping' }],
        ]);
        break;

      case 'rank':
        await sendTelegramKB(env, chatId, await cmdRank(env, 'cpu'), [
          [{ text: '🔲 CPU', callback_data: 'rank:cpu' }, { text: '💾 内存', callback_data: 'rank:mem' }, { text: '🌐 网络', callback_data: 'rank:net' }],
          [{ text: '🔄 刷新', callback_data: 'cmd:rank' }],
        ]);
        break;

      case 'info':
        await sendTelegramKB(env, chatId, await cmdInfo(env), [
          [{ text: '🔄 刷新', callback_data: 'cmd:info' }],
        ]);
        break;

      case 'group':
        await cmdGroupWithButtons(env, chatId);
        break;

      case 'admin':
        await sendTelegramKB(env, chatId, await cmdAdmin(env), [
          [{ text: '📋 客户端', callback_data: 'adm_cl' }, { text: '🔔 通知', callback_data: 'adm_no' }],
          [{ text: '📡 Ping', callback_data: 'adm_pt' }, { text: '📋 任务', callback_data: 'adm_tl' }],
          [{ text: '📜 日志', callback_data: 'adm_log' }, { text: '⚙️ 设置', callback_data: 'adm_set' }],
          [{ text: '🔐 会话', callback_data: 'adm_sess' }],
        ]);
        break;

      case 'notify':
        await sendTelegramKB(env, chatId, await cmdAdminNotify(env), [
          [{ text: '🔄 刷新', callback_data: 'adm_no' }],
        ]);
        break;

      case 'ping_admin':
        await sendTelegramKB(env, chatId, await cmdAdminPingTasks(env), [
          [{ text: '🔄 刷新', callback_data: 'adm_pt' }],
        ]);
        break;

      case 'task':
        await sendTelegramKB(env, chatId, await cmdAdminTasks(env), [
          [{ text: '🔄 刷新', callback_data: 'adm_tl' }],
        ]);
        break;

      case 'logs':
        await sendTelegramKB(env, chatId, await cmdAdminLogs(env, 1), [
          [{ text: '🔄 刷新', callback_data: 'adm_log' }],
        ]);
        break;

      case 'settings':
        await sendTelegramKB(env, chatId, await cmdAdminSettings(env), [
          [{ text: '🔄 刷新', callback_data: 'adm_set' }],
        ]);
        break;

      case 'sessions':
        await sendTelegramKB(env, chatId, await cmdAdminSessions(env), [
          [{ text: '🔄 刷新', callback_data: 'adm_sess' }],
        ]);
        break;

      case 'edit':
        await handleEditCommand(env, chatId, args);
        break;

      default:
        // Try matching as node name
        const found = await findNodeByName(env, text);
        if (found) {
          const result = await cmdNode(env, found.uuid);
          if (result) {
            await sendTelegramKB(env, chatId, result.text, [
              [{ text: '✏️ 编辑', callback_data: `adm_ce:${found.uuid}` }, { text: '🔑 Token', callback_data: `adm_ct:${found.uuid}` }],
              [{ text: '📈 历史', callback_data: `history:${found.uuid}` }, { text: '🔄 刷新', callback_data: `node:${found.uuid}` }],
              [{ text: '📋 返回列表', callback_data: 'cmd:list' }],
            ]);
            break;
          }
        }
        await sendTelegram(env, chatId, `未知命令: /${cmd}\n发送 /help 查看可用命令`);
    }
    return;
  }

  // Non-command text: try matching as node name
  const found = await findNodeByName(env, text);
  if (found) {
    const result = await cmdNode(env, found.uuid);
    if (result) {
      await sendTelegramKB(env, chatId, result.text, [
        [{ text: '✏️ 编辑', callback_data: `adm_ce:${found.uuid}` }, { text: '🔑 Token', callback_data: `adm_ct:${found.uuid}` }],
        [{ text: '📈 历史', callback_data: `history:${found.uuid}` }, { text: '🔄 刷新', callback_data: `node:${found.uuid}` }],
        [{ text: '📋 返回列表', callback_data: 'cmd:list' }],
      ]);
      return;
    }
  }

  // Forward to WeCom (optional)
  if (env.WECOM_CID && env.WECOM_SECRET) {
    const result = await sendWeCom(env, env.WECOM_TOUID || '@all', env.WECOM_AID || '', text);
    if (result.ok) {
      await sendTelegram(env, chatId, '✅ 已转发到企业微信');
    } else {
      await sendTelegram(env, chatId, `❌ 转发失败: ${result.error || 'unknown'}`);
    }
  }
}

async function findNodeByName(env, name) {
  const nodes = await komariReq(env, '/api/nodes');
  if (!nodes) return null;
  const lower = name.toLowerCase();
  return nodes.find(n => n.name && n.name.toLowerCase() === lower) ||
         nodes.find(n => n.name && n.name.toLowerCase().includes(lower)) || null;
}

async function cmdListWithButtons(env, chatId) {
  const { nodes, rtMap } = await getNodeListAndRT(env);
  if (!nodes) { await sendTelegram(env, chatId, '❌ 无法获取节点列表'); return; }
  const buttons = [];
  for (const n of nodes) {
    if (!n.hidden) {
      buttons.push([{ text: n.name, callback_data: 'node:' + n.uuid }]);
      if (buttons.length >= 20) break;
    }
  }
  buttons.push([{ text: '🔄 刷新', callback_data: 'cmd:list' }]);
  await sendTelegramKB(env, chatId, fmtNodeList(nodes, rtMap || {}), buttons);
}

async function cmdOfflineWithButtons(env, chatId) {
  const { nodes, rtMap } = await getNodeListAndRT(env);
  if (!nodes) { await sendTelegram(env, chatId, '❌ 无法获取节点列表'); return; }
  const offline = nodes.filter(n => !n.hidden && !(rtMap || {})[n.uuid]);
  const buttons = [];
  for (const n of offline) {
    buttons.push([{ text: n.name, callback_data: 'node:' + n.uuid }]);
    if (buttons.length >= 20) break;
  }
  buttons.push([{ text: '🔄 刷新', callback_data: 'cmd:offline' }]);
  await sendTelegramKB(env, chatId, fmtOfflineNodes(nodes, rtMap || {}), buttons);
}

async function cmdGroupWithButtons(env, chatId) {
  const { nodes } = await getNodeListAndRT(env);
  if (!nodes) { await sendTelegram(env, chatId, '❌ 无法获取节点列表'); return; }
  const groups = {};
  for (const n of nodes) {
    const g = n.group || '未分组';
    if (!groups[g]) groups[g] = [];
    groups[g].push(n);
  }
  const buttons = [];
  for (const g of Object.keys(groups)) {
    buttons.push([{ text: `${g} (${groups[g].length})`, callback_data: 'group:' + g }]);
  }
  buttons.push([{ text: '🔄 刷新', callback_data: 'cmd:group' }]);
  await sendTelegramKB(env, chatId, fmtGroupList(nodes), buttons);
}

// ─── Callback Handler ──────────────────────────────────────

async function handleCallbackData(env, chatId, msgId, data) {
  const colonIdx = data.indexOf(':');
  const act = colonIdx >= 0 ? data.slice(0, colonIdx) : data;
  const param = colonIdx >= 0 ? data.slice(colonIdx + 1) : '';

  switch (act) {
    case 'cmd':
      switch (param) {
        case 'status':
          await sendTelegramKB(env, chatId, await cmdStatus(env), [
            [{ text: '🔄 刷新', callback_data: 'cmd:status' }, { text: '📋 列表', callback_data: 'cmd:list' }],
            [{ text: '🔴 离线', callback_data: 'cmd:offline' }, { text: '🏆 排名', callback_data: 'cmd:rank' }],
          ]);
          break;
        case 'list':
          await cmdListWithButtons(env, chatId);
          break;
        case 'offline':
          await cmdOfflineWithButtons(env, chatId);
          break;
        case 'ping':
          await sendTelegramKB(env, chatId, await cmdPing(env), [
            [{ text: '🔄 刷新', callback_data: 'cmd:ping' }],
          ]);
          break;
        case 'rank':
          await sendTelegramKB(env, chatId, await cmdRank(env, 'cpu'), [
            [{ text: '🔲 CPU', callback_data: 'rank:cpu' }, { text: '💾 内存', callback_data: 'rank:mem' }, { text: '🌐 网络', callback_data: 'rank:net' }],
            [{ text: '🔄 刷新', callback_data: 'cmd:rank' }],
          ]);
          break;
        case 'info':
          await sendTelegramKB(env, chatId, await cmdInfo(env), [
            [{ text: '🔄 刷新', callback_data: 'cmd:info' }],
          ]);
          break;
        case 'group':
          await cmdGroupWithButtons(env, chatId);
          break;
        case 'help':
          await sendTelegramKB(env, chatId, HELP_TEXT, HELP_BUTTONS);
          break;
        default:
          await sendTelegram(env, chatId, `未知操作: ${param}`);
      }
      break;

    case 'node': {
      const result = await cmdNode(env, param);
      if (!result) { await sendTelegram(env, chatId, '❌ 节点不存在'); break; }
      await sendTelegramKB(env, chatId, result.text, [
        [{ text: '✏️ 编辑', callback_data: `adm_ce:${param}` }, { text: '🔑 Token', callback_data: `adm_ct:${param}` }],
        [{ text: '📈 历史', callback_data: `history:${param}` }, { text: '🔄 刷新', callback_data: `node:${param}` }],
        [{ text: '📋 返回列表', callback_data: 'cmd:list' }],
      ]);
      break;
    }

    case 'history': {
      const txt = await cmdHistory(env, param);
      await sendTelegramKB(env, chatId, txt, [
        [{ text: '🔄 刷新', callback_data: `history:${param}` }, { text: '📋 详情', callback_data: `node:${param}` }],
      ]);
      break;
    }

    case 'rank':
      await sendTelegramKB(env, chatId, await cmdRank(env, param), [
        [{ text: '🔲 CPU', callback_data: 'rank:cpu' }, { text: '💾 内存', callback_data: 'rank:mem' }, { text: '🌐 网络', callback_data: 'rank:net' }],
        [{ text: '🔄 刷新', callback_data: `rank:${param}` }],
      ]);
      break;

    case 'group': {
      const { nodes, rtMap } = await getNodeListAndRT(env);
      if (!nodes) { await sendTelegram(env, chatId, '❌ 无法获取节点列表'); break; }
      const groupNodes = nodes.filter(n => (n.group || '未分组') === param);
      if (groupNodes.length === 0) { await sendTelegram(env, chatId, `📂 分组 '${param}' 暂无节点`); break; }
      let txt = `📂 *${param}* (${groupNodes.length}个节点)\n\n`;
      for (let i = 0; i < groupNodes.length; i++) {
        const n = groupNodes[i];
        const emoji = (rtMap || {})[n.uuid] ? '🟢' : '🔴';
        txt += `${i + 1}. ${emoji} ${n.name}`;
        if (n.region) txt += ` ${n.region}`;
        txt += '\n';
      }
      const buttons = [];
      for (const n of groupNodes) {
        buttons.push([{ text: n.name, callback_data: 'node:' + n.uuid }]);
        if (buttons.length >= 20) break;
      }
      buttons.push([{ text: '⬅️ 返回分组', callback_data: 'cmd:group' }]);
      await sendTelegramKB(env, chatId, txt, buttons);
      break;
    }

    // Admin callbacks
    case 'adm':
      await sendTelegramKB(env, chatId, await cmdAdmin(env), [
        [{ text: '📋 客户端', callback_data: 'adm_cl' }, { text: '🔔 通知', callback_data: 'adm_no' }],
        [{ text: '📡 Ping', callback_data: 'adm_pt' }, { text: '📋 任务', callback_data: 'adm_tl' }],
        [{ text: '📜 日志', callback_data: 'adm_log' }, { text: '⚙️ 设置', callback_data: 'adm_set' }],
        [{ text: '🔐 会话', callback_data: 'adm_sess' }],
      ]);
      break;

    case 'adm_cl': {
      const data = await komariAdminReq(env, 'GET', '/api/admin/client/list');
      if (!data || !Array.isArray(data)) { await sendTelegram(env, chatId, '❌ 无法获取客户端列表'); break; }
      const buttons = [];
      for (const c of data) {
        if (c.name) buttons.push([{ text: c.name, callback_data: 'adm_cd:' + c.uuid }]);
        if (buttons.length >= 20) break;
      }
      buttons.push([{ text: '➕ 添加', callback_data: 'adm_ca' }]);
      buttons.push([{ text: '🔄 刷新', callback_data: 'adm_cl' }]);
      await sendTelegramKB(env, chatId, await cmdAdminClients(env), buttons);
      break;
    }

    case 'adm_cd': {
      const txt = await cmdAdminClientDetail(env, param);
      await sendTelegramKB(env, chatId, txt, [
        [{ text: '🔑 Token', callback_data: `adm_ct:${param}` }, { text: '🗑 删除', callback_data: `adm_crm:${param}` }],
        [{ text: '⬅️ 返回列表', callback_data: 'adm_cl' }],
      ]);
      break;
    }

    case 'adm_ct': {
      const txt = await cmdAdminClientToken(env, param);
      await sendTelegramKB(env, chatId, txt, [
        [{ text: '⬅️ 返回详情', callback_data: `adm_cd:${param}` }],
      ]);
      break;
    }

    case 'adm_crm':
      await sendTelegramKB(env, chatId, `⚠️ 确认删除此客户端？`, [
        [{ text: '✅ 确认删除', callback_data: `adm_crm_y:${param}` }, { text: '❌ 取消', callback_data: `adm_cd:${param}` }],
      ]);
      break;

    case 'adm_crm_y': {
      const result = await komariAdminReq(env, 'POST', `/api/admin/client/${param}/remove`);
      await sendTelegram(env, chatId, result !== null ? '✅ 客户端已删除' : '❌ 删除失败');
      break;
    }

    case 'adm_no':
      await sendTelegramKB(env, chatId, await cmdAdminNotify(env), [
        [{ text: '🔄 刷新', callback_data: 'adm_no' }],
      ]);
      break;

    case 'adm_pt':
      await sendTelegramKB(env, chatId, await cmdAdminPingTasks(env), [
        [{ text: '🔄 刷新', callback_data: 'adm_pt' }],
      ]);
      break;

    case 'adm_tl':
      await sendTelegramKB(env, chatId, await cmdAdminTasks(env), [
        [{ text: '🔄 刷新', callback_data: 'adm_tl' }],
      ]);
      break;

    case 'adm_log':
      await sendTelegramKB(env, chatId, await cmdAdminLogs(env, 1), [
        [{ text: '🔄 刷新', callback_data: 'adm_log' }],
      ]);
      break;

    case 'adm_logp': {
      const page = parseInt(param) || 1;
      await sendTelegramKB(env, chatId, await cmdAdminLogs(env, page), [
        page > 1 ? [{ text: '⬅️ 上一页', callback_data: `adm_logp:${page - 1}` }] : [],
        [{ text: '➡️ 下一页', callback_data: `adm_logp:${page + 1}` }],
        [{ text: '🔄 刷新', callback_data: `adm_logp:${page}` }],
      ].filter(r => r.length > 0));
      break;
    }

    case 'adm_set':
      await sendTelegramKB(env, chatId, await cmdAdminSettings(env), [
        [{ text: '🔄 刷新', callback_data: 'adm_set' }],
      ]);
      break;

    case 'adm_sess':
      await sendTelegramKB(env, chatId, await cmdAdminSessions(env), [
        [{ text: '🔄 刷新', callback_data: 'adm_sess' }],
      ]);
      break;

    case 'adm_ce': {
      console.log('[adm_ce] param:', param);
      const data = await komariAdminReq(env, 'GET', `/api/admin/client/${param}`);
      console.log('[adm_ce] data:', JSON.stringify(data));
      if (!data) { await sendTelegram(env, chatId, '❌ 无法获取客户端'); break; }
      const c = Array.isArray(data) ? data[0] : data;
      if (!c) { await sendTelegram(env, chatId, '❌ 客户端不存在'); break; }
      const short = param.slice(0, 8);
      const editHelp = `✏️ *Edit ${c.name || 'client'}* / ${short}\n\n` +
        `Current values:\n` +
        `• name: ${c.name || '-'}\n` +
        `• group: ${c.group || '-'}\n` +
        `• region: ${c.region || '-'}\n` +
        `• \`public_remark\`: ${c.public_remark || '-'}\n\n` +
        `Tap a field button below, type the new value, then send:`;
      const buttons = [
        [{ text: `📝 name: ${c.name || '-'}`, switch_inline_query_current_chat: `/edit ${short} name=` }],
        [{ text: `📂 group: ${c.group || '-'}`, switch_inline_query_current_chat: `/edit ${short} group=` }],
        [{ text: `🌍 region: ${c.region || '-'}`, switch_inline_query_current_chat: `/edit ${short} region=` }],
        [{ text: `🏷️ public_remark: ${c.public_remark || '-'}`, switch_inline_query_current_chat: `/edit ${short} public_remark=` }],
        [{ text: '📋 Detail', callback_data: `adm_cd:${param}` }, { text: '⬅️ Back', callback_data: 'adm_cl' }],
      ];
      if (msgId) {
        console.log('[adm_ce] editing msg:', msgId);
        const result = await editTelegramMessage(env, chatId, msgId, editHelp, buttons);
        console.log('[adm_ce] edit result:', JSON.stringify(result));
        if (!result.ok) {
          // fallback: send as new message
          await sendTelegramKB(env, chatId, editHelp, buttons);
        }
      } else {
        await sendTelegramKB(env, chatId, editHelp, buttons);
      }
      break;
    }

    default:
      await sendTelegram(env, chatId, `未知回调: ${data}`);
  }
}

// ─── Telegram API ──────────────────────────────────────────

async function answerCallback(env, callbackId, text = '') {
  const apiBase = env.TELEGRAM_API_BASE || 'https://api.telegram.org';
  const body = { callback_query_id: callbackId };
  if (text) body.text = text;
  await fetch(`${apiBase}/bot${env.TELEGRAM_BOT_TOKEN}/answerCallbackQuery`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

async function sendTelegram(env, chatId, text, parseMode = 'Markdown') {
  const apiBase = env.TELEGRAM_API_BASE || 'https://api.telegram.org';
  const body = { chat_id: chatId, text };
  if (parseMode) body.parse_mode = parseMode;

  const resp = await fetch(`${apiBase}/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) console.error('[sendTelegram]', resp.status, JSON.stringify(data));
  return { ok: resp.ok, status: resp.status, data };
}

async function editTelegramMessage(env, chatId, messageId, text, buttons) {
  const apiBase = env.TELEGRAM_API_BASE || 'https://api.telegram.org';
  const body = {
    chat_id: chatId,
    message_id: messageId,
    text,
    parse_mode: 'Markdown',
    reply_markup: { inline_keyboard: buttons },
  };
  const resp = await fetch(`${apiBase}/bot${env.TELEGRAM_BOT_TOKEN}/editMessageText`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) console.error('[editTelegramMessage]', resp.status, JSON.stringify(data));
  return { ok: resp.ok, status: resp.status, data };
}

async function sendTelegramKB(env, chatId, text, buttons) {
  const apiBase = env.TELEGRAM_API_BASE || 'https://api.telegram.org';
  const body = {
    chat_id: chatId,
    text,
    parse_mode: 'Markdown',
    reply_markup: { inline_keyboard: buttons },
  };

  const resp = await fetch(`${apiBase}/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

  const data = await resp.json().catch(() => ({}));
  return { ok: resp.ok, status: resp.status, data };
}

// ─── WeCom ─────────────────────────────────────────────────

async function getWeComToken(env) {
  if (env.KV) {
    const cached = await env.KV.get('wecom_token');
    if (cached) {
      const { token, expires } = JSON.parse(cached);
      if (Date.now() < expires) return token;
    }
  }

  const resp = await fetch(
    `https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=${env.WECOM_CID}&corpsecret=${env.WECOM_SECRET}`
  );
  const data = await resp.json().catch(() => ({}));

  if (!data.access_token) {
    throw new Error(`WeCom token error: ${data.errmsg || 'unknown'}`);
  }

  if (env.KV) {
    await env.KV.put('wecom_token', JSON.stringify({
      token: data.access_token,
      expires: Date.now() + (data.expires_in - 300) * 1000,
    }), { expirationTtl: 7200 });
  }

  return data.access_token;
}

async function sendWeCom(env, toUser, agentId, msg) {
  try {
    const token = await getWeComToken(env);
    const resp = await fetch(
      `https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=${token}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          touser: toUser,
          agentid: agentId,
          msgtype: 'text',
          text: { content: msg },
        }),
      }
    );

    const data = await resp.json().catch(() => ({}));
    if (data.errcode !== 0) {
      console.error('[sendWeCom]', data.errcode, data.errmsg);
    }
    return { ok: data.errcode === 0, data };
  } catch (err) {
    console.error('[sendWeCom]', err.message);
    return { ok: false, error: err.message };
  }
}

function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
    },
  });
}
