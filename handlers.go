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
		{"command": "offline", "description": "离线节点"},
		{"command": "ping", "description": "Ping任务信息"},
		{"command": "rank", "description": "资源使用排名"},
		{"command": "info", "description": "站点信息"},
		{"command": "group", "description": "节点分组"},
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
		tgSendKB(chatID, "*Komari 监控 Bot*\n\n"+
			"/status - 状态概览\n"+
			"/list - 节点列表\n"+
			"/offline - 离线节点\n"+
			"/ping - Ping任务\n"+
			"/rank - 资源排名\n"+
			"/info - 站点信息\n"+
			"/group - 节点分组\n"+
			"/help - 帮助\n\n"+
			"直接输入节点名称查看详情",
			[][]InlineButton{
				{{Text: "📊 状态", CallbackData: "cmd:status"}, {Text: "📋 列表", CallbackData: "cmd:list"}},
				{{Text: "🔴 离线", CallbackData: "cmd:offline"}, {Text: "📡 Ping", CallbackData: "cmd:ping"}},
				{{Text: "🏆 排名", CallbackData: "cmd:rank"}, {Text: "ℹ️ 信息", CallbackData: "cmd:info"}},
			})
	case "status":
		handleTgStatus(chatID)
	case "list":
		handleTgList(chatID)
	case "offline":
		handleTgOffline(chatID)
	case "ping":
		handleTgPing(chatID)
	case "rank":
		handleTgRank(chatID)
	case "info":
		handleTgInfo(chatID)
	case "group":
		handleTgGroup(chatID)
	default:
		tgSend(chatID, "未知命令: /"+cmd)
	}
}

func handleTgStatus(chatID int64) {
	nodes, err := getNodeList()
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	total := len(nodes)
	online := 0
	offline := 0
	var totalCPU float64
	var totalMemUsed, totalMemTotal uint64
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		rt, err := getNodeRealtime(n.UUID)
		if err == nil && rt != nil {
			online++
			totalCPU += rt.CPU.Usage
			totalMemUsed += rt.RAM.Used
			totalMemTotal += n.MemTotal
		} else {
			offline++
		}
	}
	var avgCPU, avgMem float64
	if online > 0 {
		avgCPU = totalCPU / float64(online)
		if totalMemTotal > 0 {
			avgMem = float64(totalMemUsed) / float64(totalMemTotal) * 100
		}
	}
	txt := fmt.Sprintf("📊 *节点状态概览*\n\n"+
		"📋 总数: %d\n"+
		"🟢 在线: %d\n"+
		"🔴 离线: %d\n\n"+
		"🔲 平均CPU: %.1f%%\n"+
		"💾 平均内存: %.1f%%",
		total, online, offline, avgCPU, avgMem)
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🔄 刷新", CallbackData: "cmd:status"}, {Text: "📋 列表", CallbackData: "cmd:list"}},
		{{Text: "🔴 离线", CallbackData: "cmd:offline"}, {Text: "🏆 排名", CallbackData: "cmd:rank"}},
	})
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

func handleTgOffline(chatID int64) {
	offline, err := getOfflineNodes()
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	txt := fmtOfflineNodes()
	var btns [][]InlineButton
	for _, n := range offline {
		btns = append(btns, []InlineButton{{Text: n.Name, CallbackData: "node:" + n.UUID}})
		if len(btns) >= 20 {
			break
		}
	}
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: "cmd:offline"}})
	tgSendKB(chatID, txt, btns)
}

func handleTgPing(chatID int64) {
	txt := fmtPingInfo()
	tgSendKB(chatID, txt, [][]InlineButton{{{Text: "🔄 刷新", CallbackData: "cmd:ping"}}})
}

func handleTgRank(chatID int64) {
	nodes, err := getNodeList()
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	txt := fmtCPUUsageRank(nodes)
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🔲 CPU", CallbackData: "rank:cpu"}, {Text: "💾 内存", CallbackData: "rank:mem"}, {Text: "🌐 网络", CallbackData: "rank:net"}},
		{{Text: "🔄 刷新", CallbackData: "cmd:rank"}},
	})
}

func handleTgInfo(chatID int64) {
	txt := fmtSiteInfo()
	tgSendKB(chatID, txt, [][]InlineButton{{{Text: "🔄 刷新", CallbackData: "cmd:info"}}})
}

func handleTgGroup(chatID int64) {
	groups := getNodeGroups()
	if len(groups) == 0 {
		tgSend(chatID, "暂无分组")
		return
	}
	txt := fmtGroupList()
	var btns [][]InlineButton
	for _, g := range groups {
		btns = append(btns, []InlineButton{{Text: g, CallbackData: "group:" + g}})
	}
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: "cmd:group"}})
	tgSendKB(chatID, txt, btns)
}

func handleTgGroupNodes(chatID int64, groupName string) {
	nodes := getNodesByGroup(groupName)
	if len(nodes) == 0 {
		tgSend(chatID, fmt.Sprintf("📂 分组 '%s' 暂无节点", groupName))
		return
	}
	txt := fmt.Sprintf("📂 *%s* (%d个节点)\n\n", groupName, len(nodes))
	for i, n := range nodes {
		rt, _ := getNodeRealtime(n.UUID)
		emoji := "🔴"
		if rt != nil {
			emoji = "🟢"
		}
		txt += fmt.Sprintf("%d. %s %s", i+1, emoji, n.Name)
		if n.Region != "" {
			txt += " " + n.Region
		}
		txt += "\n"
	}
	var btns [][]InlineButton
	for _, n := range nodes {
		btns = append(btns, []InlineButton{{Text: n.Name, CallbackData: "node:" + n.UUID}})
		if len(btns) >= 20 {
			break
		}
	}
	btns = append(btns, []InlineButton{{Text: "⬅️ 返回分组", CallbackData: "cmd:group"}})
	tgSendKB(chatID, txt, btns)
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
	case "rank":
		handleTgRankSwitch(uid, param)
	case "group":
		handleTgGroupNodes(uid, param)
	}
}

func handleTgRankSwitch(chatID int64, typ string) {
	nodes, err := getNodeList()
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	var txt string
	switch typ {
	case "cpu":
		txt = fmtCPUUsageRank(nodes)
	case "mem":
		txt = fmtMemUsageRank(nodes)
	case "net":
		txt = fmtNetUsageRank(nodes)
	default:
		txt = fmtCPUUsageRank(nodes)
	}
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🔲 CPU", CallbackData: "rank:cpu"}, {Text: "💾 内存", CallbackData: "rank:mem"}, {Text: "🌐 网络", CallbackData: "rank:net"}},
		{{Text: "🔄 刷新", CallbackData: "rank:" + typ}},
	})
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
		return "可用命令:\n" +
			"/status - 状态\n" +
			"/list - 列表\n" +
			"/offline - 离线节点\n" +
			"/ping - Ping任务\n" +
			"/rank - 排名\n" +
			"/info - 站点信息\n" +
			"/group - 分组\n" +
			"/help - 帮助\n" +
			"直接输入节点名称查看详情"
	case content == "状态" || content == "/status" || content == "status":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		total := len(nodes)
		online := 0
		offline := 0
		var totalCPU float64
		var totalMemUsed, totalMemTotal uint64
		for _, n := range nodes {
			if n.Hidden {
				continue
			}
			rt, err := getNodeRealtime(n.UUID)
			if err == nil && rt != nil {
				online++
				totalCPU += rt.CPU.Usage
				totalMemUsed += rt.RAM.Used
				totalMemTotal += n.MemTotal
			} else {
				offline++
			}
		}
		var avgCPU, avgMem float64
		if online > 0 {
			avgCPU = totalCPU / float64(online)
			if totalMemTotal > 0 {
				avgMem = float64(totalMemUsed) / float64(totalMemTotal) * 100
			}
		}
		return fmt.Sprintf("📊 节点状态概览\n\n"+
			"📋 总数: %d\n"+
			"🟢 在线: %d\n"+
			"🔴 离线: %d\n\n"+
			"🔲 平均CPU: %.1f%%\n"+
			"💾 平均内存: %.1f%%",
			total, online, offline, avgCPU, avgMem)
	case content == "列表" || content == "/list" || content == "list":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtNodeListWithStatus(nodes)
	case content == "离线" || content == "/offline" || content == "offline":
		return fmtOfflineNodes()
	case content == "ping" || content == "/ping" || content == "Ping":
		return fmtPingInfo()
	case content == "排名" || content == "/rank" || content == "rank":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtCPUUsageRank(nodes) + "\n\n" + fmtMemUsageRank(nodes)
	case content == "信息" || content == "/info" || content == "info":
		return fmtSiteInfo()
	case content == "分组" || content == "/group" || content == "group":
		return fmtGroupList()
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
