// Komari 通知脚本 - 转发到 wecom-komari（Telegram + 企业微信）
// 在 Komari 后台「设置 → 通知」中粘贴此脚本
//
// ⚠️ 使用前修改下面 4 个配置项
//
// Komari 调用方式:
//   sendMessage(message, title)  — 纯文本通知
//   sendEvent(event)             — 事件通知（上线/离线/告警等）

// ============ 配置项（必改） ============
const WECOM_KOMARI_URL = "http://your-server:8080";  // wecom-komari 服务地址（不带尾部 /）
const SENDKEY = "your-sendkey";                       // 认证密钥（对应 wecom-komari 的 SENDKEY 环境变量）
const TG_CHAT_ID = 0;                                  // Telegram Chat ID（数字，不能为 0）
const WECOM_USER = "@all";                            // 企业微信接收人（@all=所有人）

// ============ 事件 emoji 映射 ============
const EVENT_EMOJI = {
    "Offline": "🔴",
    "Online": "🟢",
    "Alert": "⚠️",
    "Renew": "⏰",
    "Expire": "🚨",
    "Test": "🧪"
};

const EVENT_NAME = {
    "Offline": "离线",
    "Online": "上线",
    "Alert": "告警",
    "Renew": "续费",
    "Expire": "到期",
    "Test": "测试"
};

// ============ 辅助：安全 HTTP 请求 ============
async function safePost(url, body) {
    const opts = {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body)
    };
    try {
        const resp = await fetch(url, opts);
        const text = await resp.text();
        if (!resp.ok) {
            return { ok: false, status: resp.status, body: text };
        }
        return { ok: true, status: resp.status, body: text };
    } catch (e) {
        return { ok: false, status: 0, body: e.message };
    }
}

// ============ 格式化事件消息 ============
function formatEventMessage(event) {
    // 兼容不同字段名: event.event / event.type / event.name
    const eventType = event.event || event.type || event.name || "Unknown";
    const emoji = EVENT_EMOJI[eventType] || "📢";
    const name = EVENT_NAME[eventType] || eventType;
    const clients = event.clients || event.nodes || [];
    const time = event.time || event.timestamp || new Date().toISOString();
    const message = event.message || event.msg || "";

    let lines = [];
    lines.push(emoji + " Komari " + name + "通知");
    lines.push("⏰ " + time);

    if (clients.length > 0) {
        lines.push("🖥️ 节点 (" + clients.length + "):");
        for (var i = 0; i < clients.length; i++) {
            var c = clients[i];
            var nodeName = c.name || c.client || c.uuid || "未知";
            var ip = c.ip || c.ipv4 || "-";
            var region = c.region || "";
            var load = c.load ? (" | 负载 " + c.load) : "";
            lines.push("  • " + nodeName + " (" + ip + ")" + region + load);
        }
    }

    if (message) {
        lines.push("📝 " + message);
    }

    return lines.join("\n");
}

// ============ 发送到 wecom-komari ============
async function postToWecomKomari(text) {
    var errors = [];

    // 发 Telegram（跳过 chat_id 为 0 的情况）
    if (TG_CHAT_ID && TG_CHAT_ID !== 0) {
        var tgResult = await safePost(
            WECOM_KOMARI_URL + "/telegram/push",
            { sendkey: SENDKEY, chat_id: TG_CHAT_ID, text: text }
        );
        if (!tgResult.ok) {
            errors.push("Telegram(" + tgResult.status + "): " + tgResult.body);
        }
    }

    // 发企业微信
    if (WECOM_USER) {
        var wxResult = await safePost(
            WECOM_KOMARI_URL + "/wecomchan",
            { sendkey: SENDKEY, msg: text, touser: WECOM_USER }
        );
        if (!wxResult.ok) {
            errors.push("WeCom(" + wxResult.status + "): " + wxResult.body);
        }
    }

    if (errors.length > 0) {
        console.error("[komari-notify] " + errors.join(" | "));
        return false;
    }
    return true;
}

// ============ 必选：发送消息 ============
async function sendMessage(message, title) {
    var text = title ? (title + "\n\n" + message) : message;
    return await postToWecomKomari(text);
}

// ============ 可选：发送事件通知 ============
async function sendEvent(event) {
    var text = formatEventMessage(event);
    return await postToWecomKomari(text);
}
