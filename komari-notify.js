// Komari 通知脚本 - 转发到 wecom-komari（Telegram + 企业微信）
// 在 Komari 后台「设置 → 通知」中粘贴此脚本
//
// ⚠️ 使用前修改下面 4 个配置项

// ============ 配置项（必改） ============
const WECOM_KOMARI_URL = "http://your-server:8080";  // wecom-komari 服务地址
const SENDKEY = "your-sendkey";                       // 认证密钥
const TG_CHAT_ID = 0;                                  // Telegram Chat ID
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

// ============ 格式化事件消息 ============
function formatEventMessage(event) {
    const emoji = EVENT_EMOJI[event.event] || "📢";
    const name = EVENT_NAME[event.event] || event.event;
    const clients = event.clients || [];
    const time = event.time || new Date().toLocaleString("zh-CN", { timeZone: "Asia/Shanghai" });

    let lines = [];
    lines.push(`${emoji} Komari ${name}通知`);
    lines.push(`⏰ ${time}`);

    if (clients.length > 0) {
        lines.push(`🖥️ 节点 (${clients.length}):`);
        for (const c of clients) {
            const nodeName = c.name || c.uuid || "未知";
            const ip = c.ip || "-";
            const region = c.region || "";
            const load = c.load ? ` | 负载 ${c.load}` : "";
            lines.push(`  • ${nodeName} (${ip})${region}${load}`);
        }
    }

    if (event.message) {
        lines.push(`📝 ${event.message}`);
    }

    return lines.join("\n");
}

// ============ 发送到 wecom-komari ============
async function postToWecomKomari(text) {
    // 发 Telegram
    try {
        const tgResp = await fetch(`${WECOM_KOMARI_URL}/telegram/push`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                sendkey: SENDKEY,
                chat_id: TG_CHAT_ID,
                text: text
            })
        });
        if (!tgResp.ok) {
            console.error(`Telegram push failed: ${tgResp.status}`);
        }
    } catch (e) {
        console.error(`Telegram push error: ${e.message}`);
    }

    // 发企业微信
    try {
        const wxResp = await fetch(`${WECOM_KOMARI_URL}/wecomchan`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                sendkey: SENDKEY,
                msg: text,
                touser: WECOM_USER
            })
        });
        if (!wxResp.ok) {
            console.error(`Wecom push failed: ${wxResp.status}`);
        }
    } catch (e) {
        console.error(`Wecom push error: ${e.message}`);
    }

    return true;
}

// ============ 必选：发送消息 ============
async function sendMessage(message, title) {
    const text = title ? `${title}\n\n${message}` : message;
    return await postToWecomKomari(text);
}

// ============ 可选：发送事件通知 ============
async function sendEvent(event) {
    const text = formatEventMessage(event);
    return await postToWecomKomari(text);
}
