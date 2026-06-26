package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	Sendkey             = envDefault("SENDKEY", "set_a_sendkey")
	WecomCid            = envDefault("WECOM_CID", "")
	WecomSecret         = envDefault("WECOM_SECRET", "")
	WecomAid            = envDefault("WECOM_AID", "")
	WecomToUid          = envDefault("WECOM_TOUID", "@all")
	TelegramBotToken    = envDefault("TELEGRAM_BOT_TOKEN", "")
	TelegramWebhookSec  = envDefault("TELEGRAM_WEBHOOK_SECRET", "")
	TelegramAllowed     = envDefault("TELEGRAM_ALLOWED_USERS", "")
	TelegramAPIBase     = envDefault("TELEGRAM_API_BASE", "https://api.telegram.org")
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
	HTTPMethod        string            `json:"httpMethod"`
	Path              string            `json:"path"`
	Headers           map[string]string `json:"headers"`
	QueryStringParameters map[string]string `json:"queryStringParameters"`
	Body              string            `json:"body"`
	IsBase64Encoded   bool              `json:"isBase64Encoded"`
}

// APIGatewayResponse is the SCF API Gateway trigger response
type APIGatewayResponse struct {
	StatusCode    int               `json:"statusCode"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	IsBase64Encoded bool            `json:"isBase64Encoded"`
}

// WebhookRequest is the incoming webhook request
type WebhookRequest struct {
	Sendkey string `json:"sendkey"`
	Token   string `json:"token"`
	Text    string `json:"text"`
	Msg     string `json:"msg"`
	Content string `json:"content"`
}

// TelegramPushRequest is the Telegram push API request
type TelegramPushRequest struct {
	Sendkey string `json:"sendkey"`
	Token   string `json:"token"`
	ChatID  int64  `json:"chat_id"`
	Text    string `json:"text"`
}

// WeComChanRequest is the WeCom channel request
type WeComChanRequest struct {
	Sendkey string `json:"sendkey"`
	Token   string `json:"token"`
	Msg     string `json:"msg"`
	Content string `json:"content"`
	ToUser  string `json:"touser"`
	AgentId string `json:"agentid"`
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

// parseBody handles base64-encoded bodies
func parseBody(event events.APIGatewayRequest) string {
	body := event.Body
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(event.Body)
		if err != nil {
			log.Printf("[parseBody] base64 decode error: %v", err)
			return ""
		}
		body = string(decoded)
	}
	return body
}

// getAuthKey extracts sendkey/token from request
func getAuthKey(event events.APIGatewayRequest, reqSendkey, reqToken string) string {
	key := reqSendkey
	if key == "" {
		key = reqToken
	}
	if key == "" {
		key = event.QueryStringParameters["sendkey"]
	}
	if key == "" {
		key = event.QueryStringParameters["token"]
	}
	return key
}

// normalizePath strips API Gateway stage prefix
func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	// Strip common stage prefixes: /release, /default, /test, /prod
	stages := []string{"/release", "/default", "/test", "/prod"}
	for _, stage := range stages {
		if strings.HasPrefix(path, stage+"/") || path == stage {
			path = strings.TrimPrefix(path, stage)
			break
		}
	}
	if path == "" {
		path = "/"
	}
	return path
}

// isUserAllowed checks if a Telegram user is in the allowed list
func isUserAllowed(userID int64) bool {
	if TelegramAllowed == "" {
		return true // No restriction
	}
	for _, id := range strings.Split(TelegramAllowed, ",") {
		var allowedID int64
		fmt.Sscanf(strings.TrimSpace(id), "%d", &allowedID)
		if allowedID == userID {
			return true
		}
	}
	return false
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

	path := normalizePath(event.Path)
	log.Printf("[Handler] %s %s", event.HTTPMethod, path)

	// Route requests
	switch path {
	case "/webhook", "/":
		return handleWebhook(event)
	case "/telegram/push":
		return handleTelegramPush(event)
	case "/telegram/webhook":
		return handleTelegramWebhook(event)
	case "/wecomchan":
		return handleWeComChan(event)
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
	bodyStr := parseBody(event)
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return jsonResponse(map[string]string{"error": "invalid JSON"}, 400), nil
	}

	// Auth check
	key := getAuthKey(event, req.Sendkey, req.Token)
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
				if sendTelegram(idStr, msg, "") {
					sent = true
				}
			}
		}
	}

	// Send to WeCom
	if WecomCid != "" && WecomSecret != "" {
		if sendWeCom(WecomToUid, WecomAid, msg) {
			sent = true
		}
	}

	return jsonResponse(map[string]interface{}{"status": "ok", "sent": sent}, 200), nil
}

// handleTelegramPush handles direct Telegram push API
func handleTelegramPush(event events.APIGatewayRequest) (APIGatewayResponse, error) {
	if event.HTTPMethod != "POST" {
		return jsonResponse(map[string]string{"error": "method not allowed"}, 405), nil
	}

	var req TelegramPushRequest
	bodyStr := parseBody(event)
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return jsonResponse(map[string]string{"error": "invalid JSON"}, 400), nil
	}

	key := getAuthKey(event, req.Sendkey, req.Token)
	if key != Sendkey {
		return jsonResponse(map[string]string{"error": "unauthorized"}, 401), nil
	}

	if req.ChatID == 0 || req.Text == "" {
		return jsonResponse(map[string]string{"error": "missing chat_id or text"}, 400), nil
	}

	ok := sendTelegram(fmt.Sprintf("%d", req.ChatID), req.Text, "")
	return jsonResponse(map[string]interface{}{"status": boolStatus(ok)}, 200), nil
}

// handleWeComChan handles WeCom message sending
func handleWeComChan(event events.APIGatewayRequest) (APIGatewayResponse, error) {
	if event.HTTPMethod != "POST" {
		return jsonResponse(map[string]string{"error": "method not allowed"}, 405), nil
	}

	var req WeComChanRequest
	bodyStr := parseBody(event)
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return jsonResponse(map[string]string{"error": "invalid JSON"}, 400), nil
	}

	key := getAuthKey(event, req.Sendkey, req.Token)
	if key != Sendkey {
		return jsonResponse(map[string]string{"error": "unauthorized"}, 401), nil
	}

	msg := req.Msg
	if msg == "" {
		msg = req.Content
	}
	if msg == "" {
		return jsonResponse(map[string]string{"error": "missing msg/content"}, 400), nil
	}

	toUser := req.ToUser
	if toUser == "" {
		toUser = WecomToUid
	}
	agentId := req.AgentId
	if agentId == "" {
		agentId = WecomAid
	}

	ok := sendWeCom(toUser, agentId, msg)
	return jsonResponse(map[string]interface{}{"status": boolStatus(ok)}, 200), nil
}

func handleTelegramWebhook(event events.APIGatewayRequest) (APIGatewayResponse, error) {
	if event.HTTPMethod != "POST" {
		return jsonResponse(map[string]string{"error": "method not allowed"}, 405), nil
	}

	// Validate webhook secret
	if TelegramWebhookSec != "" {
		secret := event.Headers["X-Telegram-Bot-Api-Secret-Token"]
		if secret == "" {
			secret = event.Headers["x-telegram-bot-api-secret-token"]
		}
		if secret != TelegramWebhookSec {
			return jsonResponse(map[string]string{"error": "forbidden"}, 403), nil
		}
	}

	// Parse Telegram update
	var update struct {
		Message *struct {
			MessageID int64 `json:"message_id"`
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			From struct {
				ID        int64  `json:"id"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			} `json:"from"`
			Text string `json:"text"`
		} `json:"message"`
		CallbackQuery *struct {
			ID      string `json:"id"`
			From    struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Data    string `json:"data"`
			Message *struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"callback_query"`
	}

	bodyStr := parseBody(event)
	if err := json.Unmarshal([]byte(bodyStr), &update); err != nil {
		return jsonResponse(map[string]string{"error": "invalid JSON"}, 400), nil
	}

	// Handle callback query
	if update.CallbackQuery != nil {
		cb := update.CallbackQuery
		answerCallback(cb.ID)

		if !isUserAllowed(cb.From.ID) {
			return jsonResponse(map[string]string{"status": "ok"}, 200), nil
		}

		chatID := cb.From.ID
		if cb.Message != nil {
			chatID = cb.Message.Chat.ID
		}
		handleCallbackData(chatID, cb.Data)
		return jsonResponse(map[string]string{"status": "ok"}, 200), nil
	}

	// Handle message
	if update.Message != nil && update.Message.Text != "" {
		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)
		userID := update.Message.From.ID

		if !isUserAllowed(userID) {
			sendTelegram(fmt.Sprintf("%d", chatID), "⚠️ 无权限", "")
			return jsonResponse(map[string]string{"status": "ok"}, 200), nil
		}

		// Handle commands
		if strings.HasPrefix(text, "/") {
			parts := strings.Fields(text[1:])
			if len(parts) == 0 {
				return jsonResponse(map[string]string{"status": "ok"}, 200), nil
			}
			cmd := strings.ToLower(parts[0])
			cmd = strings.Split(cmd, "@")[0]

			switch cmd {
			case "start", "help":
				sendTelegramKB(fmt.Sprintf("%d", chatID),
					"*wecom-komari SCF Bot*\n\n"+
						"直接发送消息内容，我会转发到企业微信。\n\n"+
						"命令：\n/help - 帮助信息\n/status - 服务状态",
					[][]InlineButton{
						{{Text: "📊 状态", CallbackData: "cmd:status"}},
					})
			case "status":
				sendTelegramKB(fmt.Sprintf("%d", chatID),
					"✅ 服务运行中 (SCF)\n"+
						fmt.Sprintf("📱 Telegram: %s\n", boolStr(TelegramBotToken != ""))+
						fmt.Sprintf("💼 企业微信: %s", boolStr(WecomCid != "")),
					[][]InlineButton{
						{{Text: "🔄 刷新", CallbackData: "cmd:status"}},
					})
			default:
				sendTelegram(fmt.Sprintf("%d", chatID), fmt.Sprintf("未知命令: /%s", cmd), "")
			}
		} else {
			// Forward message to WeCom
			if WecomCid != "" && WecomSecret != "" {
				if sendWeCom(WecomToUid, WecomAid, text) {
					sendTelegram(fmt.Sprintf("%d", chatID), "✅ 已转发到企业微信", "")
				} else {
					sendTelegram(fmt.Sprintf("%d", chatID), "❌ 转发失败", "")
				}
			} else {
				sendTelegram(fmt.Sprintf("%d", chatID), "⚠️ 企业微信未配置", "")
			}
		}
	}

	return jsonResponse(map[string]string{"status": "ok"}, 200), nil
}

// InlineButton for Telegram inline keyboard
type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboard markup
type InlineKeyboard struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

// handleCallbackData routes Telegram callback button presses
func handleCallbackData(chatID int64, data string) {
	parts := strings.SplitN(data, ":", 2)
	act := parts[0]
	param := ""
	if len(parts) > 1 {
		param = parts[1]
	}

	switch act {
	case "cmd":
		switch param {
		case "status":
			sendTelegramKB(fmt.Sprintf("%d", chatID),
				"✅ 服务运行中 (SCF)\n"+
					fmt.Sprintf("📱 Telegram: %s\n", boolStr(TelegramBotToken != ""))+
					fmt.Sprintf("💼 企业微信: %s", boolStr(WecomCid != "")),
				[][]InlineButton{
					{{Text: "🔄 刷新", CallbackData: "cmd:status"}},
				})
		case "help":
			sendTelegramKB(fmt.Sprintf("%d", chatID),
				"*wecom-komari SCF Bot*\n\n"+
					"直接发送消息内容，我会转发到企业微信。\n\n"+
					"命令：\n/help - 帮助信息\n/status - 服务状态",
				[][]InlineButton{
					{{Text: "📊 状态", CallbackData: "cmd:status"}},
				})
		default:
			sendTelegram(fmt.Sprintf("%d", chatID), fmt.Sprintf("未知操作: %s", param), "")
		}
	default:
		sendTelegram(fmt.Sprintf("%d", chatID), fmt.Sprintf("未知回调: %s", data), "")
	}
}

func boolStr(b bool) string {
	if b {
		return "已配置"
	}
	return "未配置"
}

func boolStatus(b bool) string {
	if b {
		return "ok"
	}
	return "fail"
}

// sendTelegram sends a message to a Telegram chat
func sendTelegram(chatID, text, parseMode string) bool {
	url := fmt.Sprintf("%s/bot%s/sendMessage", TelegramAPIBase, TelegramBotToken)
	msg := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		msg["parse_mode"] = parseMode
	} else {
		msg["parse_mode"] = "Markdown"
	}
	body, _ := json.Marshal(msg)
	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Printf("[sendTelegram] error: %v", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[sendTelegram] status %d: %s", resp.StatusCode, string(respBody))
	}
	return resp.StatusCode == 200
}

// sendTelegramKB sends a message with inline keyboard
func sendTelegramKB(chatID, text string, buttons [][]InlineButton) bool {
	url := fmt.Sprintf("%s/bot%s/sendMessage", TelegramAPIBase, TelegramBotToken)
	msg := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
		"reply_markup": map[string]interface{}{
			"inline_keyboard": buttons,
		},
	}
	body, _ := json.Marshal(msg)
	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Printf("[sendTelegramKB] error: %v", err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func answerCallback(callbackID string) {
	url := fmt.Sprintf("%s/bot%s/answerCallbackQuery", TelegramAPIBase, TelegramBotToken)
	body, _ := json.Marshal(map[string]string{"callback_query_id": callbackID})
	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Printf("[answerCallback] error: %v", err)
		return
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)
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

func sendWeCom(toUser, agentId, msg string) bool {
	token, err := getWeComToken()
	if err != nil {
		log.Printf("[sendWeCom] get token error: %v", err)
		return false
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)
	body, _ := json.Marshal(map[string]interface{}{
		"touser":  toUser,
		"agentid": agentId,
		"msgtype": "text",
		"text":    map[string]string{"content": msg},
	})
	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Printf("[sendWeCom] error: %v", err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Errcode != 0 {
		log.Printf("[sendWeCom] errcode=%d errmsg=%s", result.Errcode, result.Errmsg)
	}
	return result.Errcode == 0
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cloudfunction.Start(Handler)
}
