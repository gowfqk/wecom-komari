package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tencentyun/scf-go-lib/cloudfunction"
	"github.com/tencentyun/scf-go-lib/events"
)

// Config from environment variables
var (
	Sendkey          = envDefault("SENDKEY", "set_a_sendkey")
	WecomCid         = envDefault("WECOM_CID", "")
	WecomSecret      = envDefault("WECOM_SECRET", "")
	WecomAid         = envDefault("WECOM_AID", "")
	WecomToUid       = envDefault("WECOM_TOUID", "@all")
	TelegramBotToken = envDefault("TELEGRAM_BOT_TOKEN", "")
	TelegramAllowed  = envDefault("TELEGRAM_ALLOWED_USERS", "")
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	// Token cache (in-memory, per instance)
	wecomTokenCache struct {
		sync.RWMutex
		token   string
		expires time.Time
	}
)

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// APIGatewayRequest is the SCF API Gateway trigger request
type APIGatewayRequest struct {
	HTTPMethod string            `json:"httpMethod"`
	Path       string            `json:"path"`
	Headers    map[string]string `json:"headers"`
	QueryStringParameters map[string]string `json:"queryStringParameters"`
	Body       string            `json:"body"`
	IsBase64Encoded bool         `json:"isBase64Encoded"`
}

// APIGatewayResponse is the SCF API Gateway trigger response
type APIGatewayResponse struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	IsBase64Encoded bool         `json:"isBase64Encoded"`
}

// WebhookRequest is the incoming webhook request
type WebhookRequest struct {
	Sendkey string `json:"sendkey"`
	Token   string `json:"token"`
	Text    string `json:"text"`
	Msg     string `json:"msg"`
	Content string `json:"content"`
}

func jsonResponse(data interface{}, status int) APIGatewayResponse {
	body, _ := json.Marshal(data)
	return APIGatewayResponse{
		StatusCode: status,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(body),
	}
}

// Handler is the main SCF handler
func Handler(ctx context.Context, event events.APIGatewayRequest) (APIGatewayResponse, error) {
	// CORS preflight
	if event.HTTPMethod == "OPTIONS" {
		return APIGatewayResponse{
			StatusCode: 204,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
				"Access-Control-Allow-Headers": "Content-Type",
			},
		}, nil
	}

	path := event.Path
	if path == "" {
		path = "/"
	}

	// Route requests
	switch path {
	case "/webhook", "/":
		return handleWebhook(event)
	case "/telegram/webhook":
		return handleTelegramWebhook(event)
	case "/healthz", "/readyz":
		return jsonResponse(map[string]string{"status": "ok"}, 200), nil
	default:
		return jsonResponse(map[string]string{"error": "not found"}, 404), nil
	}
}

func handleWebhook(event events.APIGatewayRequest) (APIGatewayResponse, error) {
	// GET = health check
	if event.HTTPMethod == "GET" {
		return jsonResponse(map[string]string{"status": "ok", "service": "wecom-komari-scf"}, 200), nil
	}

	if event.HTTPMethod != "POST" {
		return jsonResponse(map[string]string{"error": "method not allowed"}, 405), nil
	}

	// Parse request body
	var req WebhookRequest
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil {
		return jsonResponse(map[string]string{"error": "invalid JSON"}, 400), nil
	}

	// Auth check
	key := req.Sendkey
	if key == "" {
		key = req.Token
	}
	if key == "" {
		key = event.QueryStringParameters["sendkey"]
	}
	if key == "" {
		key = event.QueryStringParameters["token"]
	}
	if key != Sendkey {
		return jsonResponse(map[string]string{"error": "unauthorized"}, 401), nil
	}

	// Get message
	msg := req.Text
	if msg == "" {
		msg = req.Msg
	}
	if msg == "" {
		msg = req.Content
	}
	if msg == "" {
		return jsonResponse(map[string]string{"error": "missing text/msg/content"}, 400), nil
	}

	var sent bool

	// Send to Telegram
	if TelegramBotToken != "" && TelegramAllowed != "" {
		for _, idStr := range strings.Split(TelegramAllowed, ",") {
			idStr = strings.TrimSpace(idStr)
			if idStr != "" {
				if sendTelegram(idStr, msg) {
					sent = true
				}
			}
		}
	}

	// Send to WeCom
	if WecomCid != "" && WecomSecret != "" {
		if sendWeCom(msg) {
			sent = true
		}
	}

	return jsonResponse(map[string]interface{}{"status": "ok", "sent": sent}, 200), nil
}

func handleTelegramWebhook(event events.APIGatewayRequest) (APIGatewayResponse, error) {
	if event.HTTPMethod != "POST" {
		return jsonResponse(map[string]string{"error": "method not allowed"}, 405), nil
	}

	// Parse Telegram update
	var update struct {
		Message *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Text string `json:"text"`
		} `json:"message"`
		CallbackQuery *struct {
			ID string `json:"id"`
		} `json:"callback_query"`
	}
	if err := json.Unmarshal([]byte(event.Body), &update); err != nil {
		return jsonResponse(map[string]string{"error": "invalid JSON"}, 400), nil
	}

	// Handle callback query
	if update.CallbackQuery != nil {
		answerCallback(update.CallbackQuery.ID)
		return jsonResponse(map[string]string{"status": "ok"}, 200), nil
	}

	// Handle message
	if update.Message != nil && update.Message.Text != "" {
		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)
		userID := update.Message.From.ID

		// Check permission
		allowed := false
		for _, id := range strings.Split(TelegramAllowed, ",") {
			var allowedID int64
			fmt.Sscanf(strings.TrimSpace(id), "%d", &allowedID)
			if allowedID == userID {
				allowed = true
				break
			}
		}
		if !allowed {
			sendTelegram(fmt.Sprintf("%d", chatID), "⚠️ 无权限")
			return jsonResponse(map[string]string{"status": "ok"}, 200), nil
		}

		// Handle commands
		if strings.HasPrefix(text, "/") {
			cmd := strings.ToLower(strings.Fields(text[1:])[0])
			cmd = strings.Split(cmd, "@")[0]

			switch cmd {
			case "start", "help":
				sendTelegram(fmt.Sprintf("%d", chatID),
					"*wecom-komari SCF Bot*\n\n"+
						"直接发送消息内容，我会转发到企业微信。\n\n"+
						"命令：\n/help - 帮助信息\n/status - 服务状态")
			case "status":
				sendTelegram(fmt.Sprintf("%d", chatID),
					"✅ 服务运行中 (SCF)\n"+
						fmt.Sprintf("📱 Telegram: %s\n", boolStr(TelegramBotToken != ""))+
						fmt.Sprintf("💼 企业微信: %s", boolStr(WecomCid != "")))
			default:
				sendTelegram(fmt.Sprintf("%d", chatID), fmt.Sprintf("未知命令: /%s", cmd))
			}
		} else {
			// Forward message to WeCom
			if WecomCid != "" && WecomSecret != "" {
				if sendWeCom(text) {
					sendTelegram(fmt.Sprintf("%d", chatID), "✅ 已转发到企业微信")
				} else {
					sendTelegram(fmt.Sprintf("%d", chatID), "❌ 转发失败")
				}
			} else {
				sendTelegram(fmt.Sprintf("%d", chatID), "⚠️ 企业微信未配置")
			}
		}
	}

	return jsonResponse(map[string]string{"status": "ok"}, 200), nil
}

func boolStr(b bool) string {
	if b {
		return "已配置"
	}
	return "未配置"
}

func sendTelegram(chatID, text string) bool {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", TelegramBotToken)
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	})
	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func answerCallback(callbackID string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", TelegramBotToken)
	body, _ := json.Marshal(map[string]string{"callback_query_id": callbackID})
	httpClient.Post(url, "application/json", strings.NewReader(string(body)))
}

func getWeComToken() (string, error) {
	wecomTokenCache.RLock()
	if wecomTokenCache.token != "" && time.Now().Before(wecomTokenCache.expires) {
		token := wecomTokenCache.token
		wecomTokenCache.RUnlock()
		return token, nil
	}
	wecomTokenCache.RUnlock()

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s", WecomCid, WecomSecret)
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Errmsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("WeCom token error: %s", result.Errmsg)
	}

	wecomTokenCache.Lock()
	wecomTokenCache.token = result.AccessToken
	wecomTokenCache.expires = time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)
	wecomTokenCache.Unlock()

	return result.AccessToken, nil
}

func sendWeCom(msg string) bool {
	token, err := getWeComToken()
	if err != nil {
		return false
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)
	body, _ := json.Marshal(map[string]interface{}{
		"touser":  WecomToUid,
		"agentid": WecomAid,
		"msgtype": "text",
		"text":    map[string]string{"content": msg},
	})
	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Errcode int `json:"errcode"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Errcode == 0
}

func main() {
	cloudfunction.Start(Handler)
}
