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
      // Route requests
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

/**
 * Auth helper - check sendkey from body or query params
 */
function checkAuth(request, env, body = {}) {
  const url = new URL(request.url);
  const key = body.sendkey || body.token || url.searchParams.get('sendkey') || url.searchParams.get('token');
  return key === env.SENDKEY;
}

/**
 * Check if Telegram user is allowed
 */
function isUserAllowed(env, userId) {
  if (!env.TELEGRAM_ALLOWED_USERS) return true; // No restriction
  const allowed = env.TELEGRAM_ALLOWED_USERS.split(',').map(id => id.trim());
  return allowed.includes(String(userId));
}

/**
 * Generic webhook handler - forward messages to Telegram + WeCom
 */
async function handleWebhook(request, env) {
  // GET = health check
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

  // Get message from any supported field
  const msg = body.text || body.msg || body.content;
  if (!msg) {
    return jsonResponse({ error: 'missing text/msg/content' }, 400);
  }

  let sent = false;

  // Send to Telegram
  if (env.TELEGRAM_BOT_TOKEN && env.TELEGRAM_ALLOWED_USERS) {
    const chatIds = env.TELEGRAM_ALLOWED_USERS.split(',').map(id => id.trim()).filter(Boolean);
    for (const chatId of chatIds) {
      const result = await sendTelegram(env, chatId, msg);
      if (result.ok) sent = true;
    }
  }

  // Send to WeCom
  if (env.WECOM_CID && env.WECOM_SECRET) {
    const result = await sendWeCom(env, env.WECOM_TOUID || '@all', env.WECOM_AID || '', msg);
    if (result.ok) sent = true;
  }

  return jsonResponse({ status: 'ok', sent });
}

/**
 * Telegram push API - send message to specific chat
 */
async function handleTelegramPush(request, env) {
  if (request.method !== 'POST') {
    return jsonResponse({ error: 'method not allowed' }, 405);
  }

  const body = await request.json().catch(() => ({}));

  if (!checkAuth(request, env, body)) {
    return jsonResponse({ error: 'unauthorized' }, 401);
  }

  if (!body.chat_id || !body.text) {
    return jsonResponse({ error: 'missing chat_id or text' }, 400);
  }

  const result = await sendTelegram(env, body.chat_id, body.text);
  return jsonResponse({ status: result.ok ? 'ok' : 'fail' });
}

/**
 * WeCom channel - send message via WeCom
 */
async function handleWeComChan(request, env) {
  if (request.method !== 'POST') {
    return jsonResponse({ error: 'method not allowed' }, 405);
  }

  const body = await request.json().catch(() => ({}));

  if (!checkAuth(request, env, body)) {
    return jsonResponse({ error: 'unauthorized' }, 401);
  }

  const msg = body.msg || body.content;
  if (!msg) {
    return jsonResponse({ error: 'missing msg/content' }, 400);
  }

  const toUser = body.touser || env.WECOM_TOUID || '@all';
  const agentId = body.agentid || env.WECOM_AID || '';

  const result = await sendWeCom(env, toUser, agentId, msg);
  return jsonResponse({ status: result.ok ? 'ok' : 'fail' });
}

/**
 * Telegram Bot webhook handler
 */
async function handleTelegramWebhook(request, env) {
  if (request.method !== 'POST') {
    return jsonResponse({ error: 'method not allowed' }, 405);
  }

  // Validate webhook secret
  if (env.TELEGRAM_WEBHOOK_SECRET) {
    const secret = request.headers.get('X-Telegram-Bot-Api-Secret-Token');
    if (secret !== env.TELEGRAM_WEBHOOK_SECRET) {
      return jsonResponse({ error: 'forbidden' }, 403);
    }
  }

  const update = await request.json().catch(() => ({}));

  // Handle callback query
  if (update.callback_query) {
    const cb = update.callback_query;
    await answerCallback(env, cb.id);

    if (!isUserAllowed(env, cb.from.id)) {
      return jsonResponse({ status: 'ok' });
    }

    const chatId = cb.message ? cb.message.chat.id : cb.from.id;
    await handleCallbackData(env, chatId, cb.data);
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
  const userId = message.from.id;

  // Check if user is allowed
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
        await sendTelegramKB(env, chatId,
          '*wecom-komari Bot*\n\n' +
          '直接发送消息内容，我会转发到企业微信。\n\n' +
          '命令：\n' +
          '/help - 帮助信息\n' +
          '/status - 服务状态',
          [[{ text: '📊 状态', callback_data: 'cmd:status' }]]
        );
        break;

      case 'status':
        await sendTelegramKB(env, chatId,
          '✅ 服务运行中 (CF Workers)\n' +
          `📱 Telegram: ${env.TELEGRAM_BOT_TOKEN ? '已配置' : '未配置'}\n` +
          `💼 企业微信: ${env.WECOM_CID ? '已配置' : '未配置'}`,
          [[{ text: '🔄 刷新', callback_data: 'cmd:status' }]]
        );
        break;

      default:
        await sendTelegram(env, chatId, `未知命令: /${cmd}`);
    }
    return;
  }

  // Forward text message to WeCom
  if (env.WECOM_CID && env.WECOM_SECRET) {
    const result = await sendWeCom(env, env.WECOM_TOUID || '@all', env.WECOM_AID || '', text);
    if (result.ok) {
      await sendTelegram(env, chatId, '✅ 已转发到企业微信');
    } else {
      await sendTelegram(env, chatId, `❌ 转发失败: ${result.error || 'unknown'}`);
    }
  } else {
    await sendTelegram(env, chatId, '⚠️ 企业微信未配置');
  }
}

/**
 * Handle Telegram callback button data
 */
async function handleCallbackData(env, chatId, data) {
  const [act, param] = data.split(':');

  switch (act) {
    case 'cmd':
      switch (param) {
        case 'status':
          await sendTelegramKB(env, chatId,
            '✅ 服务运行中 (CF Workers)\n' +
            `📱 Telegram: ${env.TELEGRAM_BOT_TOKEN ? '已配置' : '未配置'}\n` +
            `💼 企业微信: ${env.WECOM_CID ? '已配置' : '未配置'}`,
            [[{ text: '🔄 刷新', callback_data: 'cmd:status' }]]
          );
          break;
        case 'help':
          await sendTelegramKB(env, chatId,
            '*wecom-komari Bot*\n\n' +
            '直接发送消息内容，我会转发到企业微信。\n\n' +
            '命令：\n/help - 帮助信息\n/status - 服务状态',
            [[{ text: '📊 状态', callback_data: 'cmd:status' }]]
          );
          break;
        default:
          await sendTelegram(env, chatId, `未知操作: ${param}`);
      }
      break;
    default:
      await sendTelegram(env, chatId, `未知回调: ${data}`);
  }
}

/**
 * Answer Telegram callback query
 */
async function answerCallback(env, callbackId) {
  const apiBase = env.TELEGRAM_API_BASE || 'https://api.telegram.org';
  await fetch(`${apiBase}/bot${env.TELEGRAM_BOT_TOKEN}/answerCallbackQuery`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ callback_query_id: callbackId }),
  });
}

/**
 * Send message to Telegram
 */
async function sendTelegram(env, chatId, text, parseMode = 'Markdown') {
  const apiBase = env.TELEGRAM_API_BASE || 'https://api.telegram.org';
  const body = {
    chat_id: chatId,
    text: text,
  };
  if (parseMode) body.parse_mode = parseMode;

  const resp = await fetch(`${apiBase}/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    console.error('[sendTelegram]', resp.status, JSON.stringify(data));
  }
  return { ok: resp.ok, status: resp.status, data };
}

/**
 * Send message with inline keyboard
 */
async function sendTelegramKB(env, chatId, text, buttons) {
  const apiBase = env.TELEGRAM_API_BASE || 'https://api.telegram.org';
  const body = {
    chat_id: chatId,
    text: text,
    parse_mode: 'Markdown',
    reply_markup: {
      inline_keyboard: buttons,
    },
  };

  const resp = await fetch(`${apiBase}/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage`, {
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
