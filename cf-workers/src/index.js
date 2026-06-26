/**
 * wecom-komari Cloudflare Worker
 * 
 * Endpoints:
 *   GET  /webhook - Health check
 *   POST /webhook - Forward message to Telegram + WeCom
 *   POST /telegram/webhook - Telegram Bot webhook
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
      // Route requests
      if (path === '/webhook' || path === '/') {
        return handleWebhook(request, env);
      }
      if (path === '/telegram/webhook') {
        return handleTelegramWebhook(request, env);
      }
      if (path === '/healthz' || path === '/readyz') {
        return jsonResponse({ status: 'ok' });
      }

      return jsonResponse({ error: 'not found' }, 404);
    } catch (err) {
      return jsonResponse({ error: err.message }, 500);
    }
  },
};

/**
 * Generic webhook handler - forward messages to Telegram + WeCom
 */
async function handleWebhook(request, env) {
  // GET = health check
  if (request.method === 'GET') {
    return jsonResponse({ status: 'ok', service: 'wecom-komari' });
  }

  if (request.method !== 'POST') {
    return jsonResponse({ error: 'method not allowed' }, 405);
  }

  const url = new URL(request.url);
  const body = await request.json().catch(() => ({}));

  // Auth check
  const key = body.sendkey || body.token || url.searchParams.get('sendkey') || url.searchParams.get('token');
  if (key !== env.SENDKEY) {
    return jsonResponse({ error: 'unauthorized' }, 401);
  }

  // Get message from any supported field
  const msg = body.text || body.msg || body.content;
  if (!msg) {
    return jsonResponse({ error: 'missing text/msg/content' }, 400);
  }

  const results = { sent: false, telegram: null, wecom: null };

  // Send to Telegram
  if (env.TELEGRAM_BOT_TOKEN && env.TELEGRAM_ALLOWED_USERS) {
    const chatIds = env.TELEGRAM_ALLOWED_USERS.split(',').map(id => id.trim()).filter(Boolean);
    for (const chatId of chatIds) {
      const result = await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId, msg);
      results.telegram = result;
      if (result.ok) results.sent = true;
    }
  }

  // Send to WeCom
  if (env.WECOM_CID && env.WECOM_SECRET) {
    const result = await sendWeCom(env, msg);
    results.wecom = result;
    if (result.ok) results.sent = true;
  }

  return jsonResponse({ status: 'ok', sent: results.sent });
}

/**
 * Telegram Bot webhook handler
 */
async function handleTelegramWebhook(request, env) {
  if (request.method !== 'POST') {
    return jsonResponse({ error: 'method not allowed' }, 405);
  }

  const update = await request.json().catch(() => ({}));

  // Handle callback query
  if (update.callback_query) {
    await handleCallback(env, update.callback_query);
    return jsonResponse({ status: 'ok' });
  }

  // Handle message
  if (update.message && update.message.text) {
    await handleMessage(env, update.message);
  }

  return jsonResponse({ status: 'ok' });
}

/**
 * Handle Telegram text message
 */
async function handleMessage(env, message) {
  const chatId = message.chat.id;
  const text = message.text.trim();

  // Check if user is allowed
  const allowedUsers = (env.TELEGRAM_ALLOWED_USERS || '').split(',').map(id => id.trim());
  if (!allowedUsers.includes(String(message.from.id))) {
    await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId, '⚠️ 无权限');
    return;
  }

  // Handle commands
  if (text.startsWith('/')) {
    const cmd = text.slice(1).split('@')[0].split(' ')[0].toLowerCase();
    const args = text.slice(1 + cmd.length).trim();

    switch (cmd) {
      case 'start':
      case 'help':
        await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId,
          '*wecom-komari Bot*\n\n' +
          '直接发送消息内容，我会转发到企业微信。\n\n' +
          '命令：\n' +
          '/help - 帮助信息\n' +
          '/status - 服务状态',
          'Markdown'
        );
        break;

      case 'status':
        await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId,
          '✅ 服务运行中\n' +
          `📱 Telegram: ${env.TELEGRAM_BOT_TOKEN ? '已配置' : '未配置'}\n` +
          `💼 企业微信: ${env.WECOM_CID ? '已配置' : '未配置'}`
        );
        break;

      default:
        await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId, `未知命令: /${cmd}`);
    }
    return;
  }

  // Forward text message to WeCom
  if (env.WECOM_CID && env.WECOM_SECRET) {
    const result = await sendWeCom(env, text);
    if (result.ok) {
      await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId, '✅ 已转发到企业微信');
    } else {
      await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId, `❌ 转发失败: ${result.error}`);
    }
  } else {
    await sendTelegram(env.TELEGRAM_BOT_TOKEN, chatId, '⚠️ 企业微信未配置');
  }
}

/**
 * Handle Telegram callback query
 */
async function handleCallback(env, callback) {
  // Answer callback to remove loading state
  await fetch(`https://api.telegram.org/bot${env.TELEGRAM_BOT_TOKEN}/answerCallbackQuery`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ callback_query_id: callback.id }),
  });
}

/**
 * Send message to Telegram
 */
async function sendTelegram(token, chatId, text, parseMode) {
  const body = {
    chat_id: chatId,
    text: text,
  };
  if (parseMode) body.parse_mode = parseMode;

  const resp = await fetch(`https://api.telegram.org/bot${token}/sendMessage`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

  const data = await resp.json().catch(() => ({}));
  return { ok: resp.ok, status: resp.status, data };
}

/**
 * Get WeCom access token (with KV caching if available)
 */
async function getWeComToken(env) {
  // Try KV cache first
  if (env.KV) {
    const cached = await env.KV.get('wecom_token');
    if (cached) {
      const { token, expires } = JSON.parse(cached);
      if (Date.now() < expires) return token;
    }
  }

  // Fetch new token
  const resp = await fetch(
    `https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=${env.WECOM_CID}&corpsecret=${env.WECOM_SECRET}`
  );
  const data = await resp.json().catch(() => ({}));

  if (!data.access_token) {
    throw new Error(`WeCom token error: ${data.errmsg || 'unknown'}`);
  }

  // Cache token (expire 5 min early)
  if (env.KV) {
    await env.KV.put('wecom_token', JSON.stringify({
      token: data.access_token,
      expires: Date.now() + (data.expires_in - 300) * 1000,
    }), { expirationTtl: 7200 });
  }

  return data.access_token;
}

/**
 * Send message to WeCom
 */
async function sendWeCom(env, msg) {
  try {
    const token = await getWeComToken(env);
    const resp = await fetch(
      `https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=${token}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          touser: env.WECOM_TOUID || '@all',
          agentid: env.WECOM_AID || '',
          msgtype: 'text',
          text: { content: msg },
        }),
      }
    );

    const data = await resp.json().catch(() => ({}));
    return { ok: data.errcode === 0, data };
  } catch (err) {
    return { ok: false, error: err.message };
  }
}

/**
 * Helper: JSON response
 */
function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
    },
  });
}
