package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// Telegram
func setTelegramBotCommands() error {
	cmds := []map[string]string{
		{"command": "status", "description": "服务器状态概览"},
		{"command": "list", "description": "所有节点列表"},
		{"command": "help", "description": "帮助信息"},
	}
	_, err := httpDo("POST", TelegramAPIBase+"/bot"+TelegramBotToken+"/setMyCommands", map[string]interface{}{"commands": cmds}, nil)
	return err
}

func telegramWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var u TgUpdate
	if json.Unmarshal(body, &u) != nil {
		w.WriteHeader(400)
		return
	}
	if u.CallbackQuery != nil {
		handleTgCallback(u.CallbackQuery)
	} else if u.Message != nil && u.Message.Text != "" {
		handleTgMsg(u.Message)
	}
	w.WriteHeader(200)
}

func handleTgMsg(m *TgMsg) {
	if !isUserAllowed(m.From.ID) {
		tgSend(m.Chat.ID, "⚠️ 无权限")
		return
	}
	txt := strings.TrimSpace(m.Text)
	if strings.HasPrefix(txt, "/") {
		handleTgCmd(m.Chat.ID, strings.TrimPrefix(txt, "/"))
		return
	}
	ns := searchNodes(txt)
	switch len(ns) {
	case 1:
		handleTgNode(m.Chat.ID, ns[0].UUID)
	case 0:
		tgSend(m.Chat.ID, fmt.Sprintf("未找到: %s", txt))
	default:
		var s strings.Builder
		s.WriteString(fmt.Sprintf("找到 %d 个节点:\n", len(ns)))
		for i, n := range ns {
			s.WriteString(fmt.Sprintf("%d. %s\n", i+1, n.Name))
		}
		tgSend(m.Chat.ID, s.String())
	}
}

func handleTgCmd(chatID int64, cmd string) {
	cmd = strings.Fields(strings.ToLower(cmd))[0]
	switch cmd {
	case "start", "help":
		tgSendKB(chatID, "*Komari 监控 Bot*\n\n/status - 状态\n/list - 节点列表\n/help - 帮助\n\n直接输入节点名称查看详情", [][]InlineButton{{{Text: "📊 状态", CallbackData: "cmd:status"}, {Text: "📋 列表", CallbackData: "cmd:list"}}})
	case "status":
		nodes, err := getNodeList()
		if err != nil {
			tgSend(chatID, "❌ "+err.Error())
			return
		}
		total := len(nodes)
		hidden := 0
		for _, n := range nodes {
			if n.Hidden {
				hidden++
			}
		}
		tgSendKB(chatID, fmt.Sprintf("📊 节点总数: %d\n👁 可见: %d | 🙈 隐藏: %d", total, total-hidden, hidden), [][]InlineButton{{{Text: "🔄 刷新", CallbackData: "cmd:status"}, {Text: "📋 列表", CallbackData: "cmd:list"}}})
	case "list":
		handleTgList(chatID)
	default:
		tgSend(chatID, "未知命令: /"+cmd)
	}
}

func handleTgList(chatID int64) {
	nodes, err := getNodeList()
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	var btns [][]InlineButton
	for _, n := range nodes {
		if !n.Hidden {
			btns = append(btns, []InlineButton{{Text: n.Name, CallbackData: "node:" + n.UUID}})
			if len(btns) >= 20 {
				break
			}
		}
	}
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: "cmd:list"}})
	tgSendKB(chatID, fmtNodeList(nodes), btns)
}

func handleTgNode(chatID int64, uuid string) {
	n, err := getNodeByUUID(uuid)
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	rt, _ := getNodeRealtime(uuid)
	tgSendKB(chatID, fmtNodeStatus(n, rt), [][]InlineButton{
		{{Text: "📈 历史", CallbackData: "history:" + uuid}, {Text: "🔄 刷新", CallbackData: "node:" + uuid}},
		{{Text: "📋 返回列表", CallbackData: "cmd:list"}},
	})
}

func handleTgCallback(cb *TgCallback) {
	answerCB(cb.ID)
	uid := cb.From.ID
	if !isUserAllowed(uid) {
		return
	}
	parts := strings.SplitN(cb.Data, ":", 2)
	act, param := parts[0], ""
	if len(parts) > 1 {
		param = parts[1]
	}
	switch act {
	case "cmd":
		handleTgCmd(uid, param)
	case "node":
		handleTgNode(uid, param)
	case "history":
		handleTgHistory(uid, param)
	}
}

func handleTgHistory(chatID int64, uuid string) {
	n, err := getNodeByUUID(uuid)
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	recs, err := getNodeLoadHistory(uuid, 1)
	if err != nil || len(recs) == 0 {
		tgSend(chatID, fmt.Sprintf("📊 %s - 暂无历史记录", n.Name))
		return
	}
	var avgCPU float64
	var avgMem, avgDisk uint64
	for _, r := range recs {
		avgCPU += r.CPU
		avgMem += r.RAM
		avgDisk += r.Disk
	}
	cnt := float64(len(recs))
	txt := fmt.Sprintf("📊 *%s - 最近1小时*\n\n🔲 平均CPU: %.1f%%\n💾 平均内存: %s\n📁 平均磁盘: %s\n📈 记录数: %d",
		n.Name, avgCPU/cnt, fmtBytes(uint64(float64(avgMem)/cnt)), fmtBytes(uint64(float64(avgDisk)/cnt)), len(recs))
	tgSendKB(chatID, txt, [][]InlineButton{{{Text: "🔄 刷新", CallbackData: "history:" + uuid}, {Text: "📋 详情", CallbackData: "node:" + uuid}}})
}

func tgSend(chatID int64, text string) {
	tgSendKB(chatID, text, nil)
}

func tgSendKB(chatID int64, text string, btns [][]InlineButton) {
	msg := TgMessage{ChatID: chatID, Text: text, ParseMode: "Markdown"}
	if len(btns) > 0 {
		msg.ReplyMarkup = &InlineKeyboard{InlineKeyboard: btns}
	}
	httpDo("POST", TelegramAPIBase+"/bot"+TelegramBotToken+"/sendMessage", msg, nil)
}

func answerCB(id string) {
	httpDo("POST", TelegramAPIBase+"/bot"+TelegramBotToken+"/answerCallbackQuery", map[string]string{"callback_query_id": id}, nil)
}

// Telegram push
func telegramPushHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Sendkey string `json:"sendkey"`
		Token   string `json:"token"`
		ChatID  int64  `json:"chat_id"`
		Text    string `json:"text"`
	}
	body, _ := io.ReadAll(r.Body)
	if json.Unmarshal(body, &req) != nil {
		w.WriteHeader(400)
		return
	}
	key := req.Sendkey
	if key == "" {
		key = req.Token
	}
	if key != Sendkey {
		w.WriteHeader(401)
		return
	}
	if req.ChatID == 0 || req.Text == "" {
		w.WriteHeader(400)
		return
	}
	tgSend(req.ChatID, req.Text)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// WeChat Work
func wecomChanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Sendkey string `json:"sendkey"`
		Token   string `json:"token"`
		Msg     string `json:"msg"`
		Content string `json:"content"`
		ToUser  string `json:"touser"`
		AgentId string `json:"agentid"`
	}
	if json.Unmarshal(body, &req) != nil {
		w.WriteHeader(400)
		return
	}
	key := req.Sendkey
	if key == "" {
		key = req.Token
	}
	if key != Sendkey {
		w.WriteHeader(401)
		return
	}
	msg := req.Msg
	if msg == "" {
		msg = req.Content
	}
	if msg == "" {
		w.WriteHeader(400)
		return
	}
	toUser := req.ToUser
	if toUser == "" {
		toUser = WecomToUid
	}
	token, err := getWecomAccessToken()
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	wd := WecomMsg{ToUser: toUser, MsgType: "text"}
	if req.AgentId != "" {
		wd.AgentId = req.AgentId
	} else {
		wd.AgentId = WecomAid
	}
	wd.Text.Content = msg
	httpDo("POST", fmt.Sprintf(SendMsgURL, token), wd, nil)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func mailHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(501)
	w.Write([]byte("not implemented"))
}

// WeChat Work callback
type WecomCallbackXML struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int      `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgId        int64    `xml:"MsgId"`
}

func wecomCallbackHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		q := r.URL.Query()
		sigs := []string{WecomToken, q.Get("timestamp"), q.Get("nonce")}
		sort.Strings(sigs)
		h := sha1.New()
		h.Write([]byte(strings.Join(sigs, "")))
		if hex.EncodeToString(h.Sum(nil)) == q.Get("signature") {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(q.Get("echostr")))
		} else {
			w.WriteHeader(403)
		}
	case "POST":
		body, _ := io.ReadAll(r.Body)
		var msg WecomCallbackXML
		xml.Unmarshal(body, &msg)
		if msg.MsgType == "text" && msg.Content != "" {
			reply := processWecomMsg(msg.Content)
			if reply != "" {
				logger.Printf("Reply to %s: %s", msg.FromUserName, reply)
			}
		}
		w.WriteHeader(200)
	default:
		w.WriteHeader(405)
	}
}

func processWecomMsg(content string) string {
	content = strings.TrimSpace(content)
	switch {
	case content == "帮助" || content == "/help" || content == "help":
		return "可用命令:\n/status - 状态\n/list - 列表\n/help - 帮助\n直接输入节点名称查看详情"
	case content == "状态" || content == "/status" || content == "status":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		total := len(nodes)
		hidden := 0
		for _, n := range nodes {
			if n.Hidden {
				hidden++
			}
		}
		return fmt.Sprintf("📊 节点总数: %d\n👁 可见: %d | 🙈 隐藏: %d", total, total-hidden, hidden)
	case content == "列表" || content == "/list" || content == "list":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtNodeList(nodes)
	default:
		ns := searchNodes(content)
		switch len(ns) {
		case 1:
			rt, _ := getNodeRealtime(ns[0].UUID)
			return fmtNodeStatus(&ns[0], rt)
		case 0:
			return fmt.Sprintf("未找到: %s", content)
		default:
			var s strings.Builder
			s.WriteString(fmt.Sprintf("找到 %d 个节点:\n", len(ns)))
			for i, n := range ns {
				s.WriteString(fmt.Sprintf("%d. %s\n", i+1, n.Name))
			}
			return s.String()
		}
	}
}

// Middleware
func recoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if e := recover(); e != nil {
				logger.Printf("Panic: %v", e)
				w.WriteHeader(500)
			}
		}()
		next(w, r)
	}
}
