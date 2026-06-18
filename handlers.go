package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
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
		{"command": "admin", "description": "管理员面板"},
		{"command": "notify", "description": "通知管理"},
		{"command": "ping_admin", "description": "Ping任务管理"},
		{"command": "task", "description": "远程任务管理"},
		{"command": "logs", "description": "审计日志"},
		{"command": "settings", "description": "Komari设置"},
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
			"/group - 节点分组\n\n"+
			"*管理命令:*\n"+
			"/admin - 管理员面板\n"+
			"/notify - 通知管理\n"+
			"/ping_admin - Ping任务管理\n"+
			"/task - 远程任务\n"+
			"/logs - 审计日志\n"+
			"/settings - 设置\n"+
			"/help - 帮助\n\n"+
			"直接输入节点名称查看详情",
			[][]InlineButton{
				{{Text: "📊 状态", CallbackData: "cmd:status"}, {Text: "📋 列表", CallbackData: "cmd:list"}},
				{{Text: "🔴 离线", CallbackData: "cmd:offline"}, {Text: "📡 Ping", CallbackData: "cmd:ping"}},
				{{Text: "🏆 排名", CallbackData: "cmd:rank"}, {Text: "ℹ️ 信息", CallbackData: "cmd:info"}},
				{{Text: "🔧 管理", CallbackData: "adm"}, {Text: "⚙️ 设置", CallbackData: "adm_set"}},
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
	case "admin":
		handleTgAdmin(chatID)
	case "notify":
		handleTgAdminNotify(chatID)
	case "ping_admin":
		handleTgAdminPingTasks(chatID)
	case "task":
		handleTgAdminTasks(chatID)
	case "logs":
		handleTgAdminLogs(chatID, 1)
	case "settings":
		handleTgAdminSettings(chatID)
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
	case "adm":
		handleTgAdmin(uid)
	case "adm_cl":
		handleTgAdminClients(uid)
	case "adm_cd":
		handleTgAdminClientDetail(uid, param)
	case "adm_ct":
		handleTgAdminClientToken(uid, param)
	case "adm_crm":
		handleTgAdminClientRemoveConfirm(uid, param)
	case "adm_crm_y":
		handleTgAdminClientRemove(uid, param)
	case "adm_no":
		handleTgAdminNotify(uid)
	case "adm_nlo":
		handleTgAdminNotifyOffline(uid)
	case "adm_nleo":
		handleTgAdminNotifyEnableOffline(uid)
	case "adm_nldo":
		handleTgAdminNotifyDisableOffline(uid)
	case "adm_nll":
		handleTgAdminNotifyLoad(uid)
	case "adm_nlt":
		handleTgAdminNotifyTraffic(uid)
	case "adm_nlet":
		handleTgAdminNotifyEnableTraffic(uid)
	case "adm_nldt":
		handleTgAdminNotifyDisableTraffic(uid)
	case "adm_pt":
		handleTgAdminPingTasks(uid)
	case "adm_ptd":
		handleTgAdminPingTaskDeleteConfirm(uid, param)
	case "adm_ptdy":
		handleTgAdminPingTaskDelete(uid, param)
	case "adm_tl":
		handleTgAdminTasks(uid)
	case "adm_td":
		handleTgAdminTaskDetail(uid, param)
	case "adm_tdr":
		handleTgAdminTaskResult(uid, param)
	case "adm_log":
		handleTgAdminLogs(uid, 1)
	case "adm_logp":
		handleTgAdminLogs(uid, parsePage(param))
	case "adm_set":
		handleTgAdminSettings(uid)
	case "adm_sess":
		handleTgAdminSessions(uid)
	case "adm_sey":
		handleTgAdminRemoveAllSessionsConfirm(uid)
	case "adm_seyy":
		handleTgAdminRemoveAllSessions(uid)
	case "adm_rec":
		handleTgAdminClearAllRecordsConfirm(uid)
	case "adm_recy":
		handleTgAdminClearAllRecords(uid)
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


func verifyWecomSignature(token, timestamp, nonce, encryptMsg, msgSignature string) bool {
	strs := sort.StringSlice{token, timestamp, nonce, encryptMsg}
	sort.Strings(strs)
	str := strings.Join(strs, "")
	hash := sha1.Sum([]byte(str))
	return fmt.Sprintf("%x", hash) == msgSignature
}

func decryptWecomMsg(encryptMsg string) (string, error) {
	if WecomAESKey == "" {
		return "", fmt.Errorf("WECOM_ENCODING_AES_KEY未配置")
	}
	encryptedBytes, err := base64.StdEncoding.DecodeString(encryptMsg)
	if err != nil {
		return "", fmt.Errorf("Base64解码失败: %v", err)
	}
	aesKey, err := base64.StdEncoding.DecodeString(WecomAESKey + "=")
	if err != nil {
		return "", fmt.Errorf("AES Key解码失败: %v", err)
	}
	if len(aesKey) != 32 {
		return "", fmt.Errorf("AES Key长度错误: 期望32字节, 实际%d字节", len(aesKey))
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("创建AES cipher失败: %v", err)
	}
	if len(encryptedBytes)%block.BlockSize() != 0 {
		return "", fmt.Errorf("密文长度不是块大小的整数倍")
	}
	iv := aesKey[:16]
	decrypted := make([]byte, len(encryptedBytes))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(decrypted, encryptedBytes)
	if len(decrypted) < 20 {
		return "", fmt.Errorf("解密后数据过短")
	}
	msgLen := binary.BigEndian.Uint32(decrypted[16:20])
	if int(msgLen) < 0 || 20+int(msgLen) > len(decrypted) {
		return "", fmt.Errorf("消息长度无效")
	}
	return string(decrypted[20 : 20+msgLen]), nil
}

type WecomCallbackXML struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int      `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgId        int64    `xml:"MsgId"`
	Encrypt      string   `xml:"Encrypt"`
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
		logger.Printf("[WecomCallback] Raw body: %s", string(body))
		var msg WecomCallbackXML
		if err := xml.Unmarshal(body, &msg); err != nil {
			logger.Printf("[WecomCallback] XML parse error: %v", err)
			w.Write([]byte("success"))
			return
		}
		logger.Printf("[WecomCallback] From=%s To=%s Type=%s Encrypt=%s", msg.FromUserName, msg.ToUserName, msg.MsgType, msg.Encrypt[:min(50, len(msg.Encrypt))])
		content := msg.Content
		if msg.Encrypt != "" {
			q := r.URL.Query()
			msgSignature := q.Get("msg_signature")
			timestamp := q.Get("timestamp")
			nonce := q.Get("nonce")
			logger.Printf("[WecomCallback] Encrypted mode, verifying signature...")
			if !verifyWecomSignature(WecomToken, timestamp, nonce, msg.Encrypt, msgSignature) {
				logger.Printf("[WecomCallback] Signature verification failed")
				w.WriteHeader(403)
				return
			}
			decrypted, err := decryptWecomMsg(msg.Encrypt)
			if err != nil {
				logger.Printf("[WecomCallback] Decrypt failed: %v", err)
				w.Write([]byte("success"))
				return
			}
			logger.Printf("[WecomCallback] Decrypted: %s", decrypted)
			var decMsg WecomCallbackXML
			xml.Unmarshal([]byte(decrypted), &decMsg)
			content = decMsg.Content
			logger.Printf("[WecomCallback] Decrypted content: %s", content)
		}
		if content != "" {
			reply := processWecomMsg(content)
			logger.Printf("[WecomCallback] Reply (len=%d): %s", len(reply), reply[:min(200, len(reply))])
			if reply != "" {
				logger.Printf("[WecomCallback] Sending reply")
				w.Header().Set("Content-Type", "application/xml")
				w.Write([]byte(fmt.Sprintf(
					"<xml><ToUserName><![CDATA[%s]]></ToUserName>"+
						"<FromUserName><![CDATA[%s]]></FromUserName>"+
						"<CreateTime>%d</CreateTime>"+
						"<MsgType><![CDATA[text]]></MsgType>"+
						"<Content><![CDATA[%s]]></Content></xml>",
					msg.FromUserName, msg.ToUserName, time.Now().Unix(), reply)))
				return
			}
		}
		w.Write([]byte("success"))
	default:
		w.WriteHeader(405)
	}
}

func processWecomMsg(content string) string {
	content = strings.TrimSpace(content)
	switch {
	case content == "帮助" || content == "/help" || content == "help":
		return "📋 查询命令:\n" +
			"/status - 节点状态\n" +
			"/list - 节点列表\n" +
			"/offline - 离线节点\n" +
			"/ping - Ping任务\n" +
			"/rank - 性能排名\n" +
			"/info - 站点信息\n" +
			"/group - 分组信息\n\n" +
			"🔧 管理命令:\n" +
			"/admin - 管理面板\n" +
			"/admin_clients - 客户端\n" +
			"/admin_notify - 通知\n" +
			"/admin_ping - Ping任务\n" +
			"/admin_tasks - 远程任务\n" +
			"/admin_logs - 审计日志\n" +
			"/admin_sessions - 会话\n" +
			"/admin_settings - 设置\n\n" +
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
	case content == "管理" || content == "/admin" || content == "admin":
		return "🔧 管理员面板\n\n" +
			"/admin_clients - 客户端管理\n" +
			"/admin_notify - 通知管理\n" +
			"/admin_ping - Ping任务管理\n" +
			"/admin_tasks - 远程任务\n" +
			"/admin_logs - 审计日志\n" +
			"/admin_sessions - 会话管理\n" +
			"/admin_settings - 设置\n" +
			"/admin_clear - 清空记录"
	case content == "/admin_clients" || content == "客户端管理":
		clients, err := adminListClients()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminClientList(clients)
	case content == "/admin_notify" || content == "通知管理":
		var sb strings.Builder
		sb.WriteString("🔔 通知管理\n\n")
		offline, err := adminListOfflineNotifications()
		if err == nil {
		sb.WriteString("📤 离线通知:\n")
		sb.WriteString(fmtAdminNotifications(offline, "离线通知"))
		sb.WriteString("\n")
		}
		traffic, err := adminListTrafficReports()
		if err == nil {
		sb.WriteString("📊 流量报告:\n")
		sb.WriteString(fmtAdminNotifications(traffic, "流量报告"))
		sb.WriteString("\n")
		}
		return sb.String()
	case content == "/admin_ping" || content == "Ping管理":
		tasks, err := adminListPingTasks()
		if err != nil {
			return "❌ " + err.Error()
		}
		var sb strings.Builder
		sb.WriteString("📡 Ping任务\n\n")
		if len(tasks) == 0 {
		sb.WriteString("暂无Ping任务")
		} else {
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("• %s (ID: %d)\n", t.Name, t.ID))
		}
		}
		return sb.String()
	case content == "/admin_tasks" || content == "远程任务":
		tasks, err := adminListAllTasks()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminTasks(tasks)
	case content == "/admin_logs" || content == "日志":
		logs, err := adminGetLogs(20, 1)
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminLogs(logs)
	case content == "/admin_sessions" || content == "会话":
		sessions, err := adminGetSessions()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminSessions(sessions)
	case content == "/admin_settings" || content == "设置":
		settings, err := adminGetSettings()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminSettings(settings)
	case content == "/admin_clear" || content == "清空记录":
		err := adminClearAllRecords()
		if err != nil {
			return "❌ 清空失败: " + err.Error()
		}
		return "✅ 所有记录已清空"
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

// Admin helpers

func parsePage(s string) int {
	if s == "" {
		return 1
	}
	var p int
	_, err := fmt.Sscanf(s, "%d", &p)
	if err != nil || p < 1 {
		return 1
	}
	return p
}

// Admin menu
func handleTgAdmin(chatID int64) {
	tgSendKB(chatID, "🔧 *管理员面板*\n\n选择要管理的功能:",
		[][]InlineButton{
			{{Text: "📋 客户端", CallbackData: "adm_cl"}, {Text: "🔔 通知", CallbackData: "adm_no"}},
			{{Text: "📡 Ping任务", CallbackData: "adm_pt"}, {Text: "⚡ 远程任务", CallbackData: "adm_tl"}},
			{{Text: "📝 审计日志", CallbackData: "adm_log"}, {Text: "🔑 会话", CallbackData: "adm_sess"}},
			{{Text: "⚙️ 设置", CallbackData: "adm_set"}},
			{{Text: "🗑 清空记录", CallbackData: "adm_rec"}, {Text: "⬅️ 返回", CallbackData: "cmd:help"}},
		})
}

// Client management
func handleTgAdminClients(chatID int64) {
	clients, err := adminListClients()
	if err != nil {
		tgSend(chatID, "❌ 获取客户端列表失败: "+err.Error())
		return
	}
	txt := fmtAdminClientList(clients)
	var btns [][]InlineButton
	for _, c := range clients {
		btns = append(btns, []InlineButton{{Text: c.Name, CallbackData: "adm_cd:" + c.UUID}})
		if len(btns) >= 15 {
			break
		}
	}
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: "adm_cl"}, {Text: "⬅️ 返回", CallbackData: "adm"}})
	tgSendKB(chatID, txt, btns)
}

func handleTgAdminClientDetail(chatID int64, uuid string) {
	client, err := adminGetClient(uuid)
	if err != nil {
		tgSend(chatID, "❌ 获取客户端详情失败: "+err.Error())
		return
	}
	txt := fmtAdminClientDetail(client)
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🔑 Token", CallbackData: "adm_ct:" + uuid}, {Text: "🗑 删除", CallbackData: "adm_crm:" + uuid}},
		{{Text: "🔄 刷新", CallbackData: "adm_cd:" + uuid}, {Text: "📋 返回列表", CallbackData: "adm_cl"}},
	})
}

func handleTgAdminClientToken(chatID int64, uuid string) {
	token, err := adminGetClientToken(uuid)
	if err != nil {
		tgSend(chatID, "❌ 获取Token失败: "+err.Error())
		return
	}
	tgSendKB(chatID, fmt.Sprintf("🔑 *客户端 Token*\n\nUUID: `%s`\nToken: `%s`", uuid, token),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_cd:" + uuid}}})
}

func handleTgAdminClientRemoveConfirm(chatID int64, uuid string) {
	client, err := adminGetClient(uuid)
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	tgSendKB(chatID, fmt.Sprintf("⚠️ *确认删除客户端?*\n\n名称: %s\nUUID: `%s`\n\n此操作不可撤销!", client.Name, uuid),
		[][]InlineButton{
			{{Text: "✅ 确认删除", CallbackData: "adm_crm_y:" + uuid}, {Text: "❌ 取消", CallbackData: "adm_cd:" + uuid}},
		})
}

func handleTgAdminClientRemove(chatID int64, uuid string) {
	err := adminRemoveClient(uuid)
	if err != nil {
		tgSend(chatID, "❌ 删除失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 客户端已删除", [][]InlineButton{{{Text: "📋 返回列表", CallbackData: "adm_cl"}}})
}

// Notification management
func handleTgAdminNotify(chatID int64) {
	tgSendKB(chatID, "🔔 *通知管理*\n\n选择通知类型:",
		[][]InlineButton{
			{{Text: "📴 离线通知", CallbackData: "adm_nlo"}, {Text: "📈 负载告警", CallbackData: "adm_nll"}},
			{{Text: "📊 流量报告", CallbackData: "adm_nlt"}},
			{{Text: "⬅️ 返回", CallbackData: "adm"}},
		})
}

func handleTgAdminNotifyOffline(chatID int64) {
	data, err := adminListOfflineNotifications()
	if err != nil {
		tgSend(chatID, "❌ 获取离线通知失败: "+err.Error())
		return
	}
	txt := fmtAdminNotifications(data, "离线通知配置")
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "✅ 启用", CallbackData: "adm_nleo"}, {Text: "⏸ 禁用", CallbackData: "adm_nldo"}},
		{{Text: "🔄 刷新", CallbackData: "adm_nlo"}, {Text: "⬅️ 返回", CallbackData: "adm_no"}},
	})
}

func handleTgAdminNotifyEnableOffline(chatID int64) {
	err := adminEnableOfflineNotification()
	if err != nil {
		tgSend(chatID, "❌ 启用失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 离线通知已启用", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlo"}}})
}

func handleTgAdminNotifyDisableOffline(chatID int64) {
	err := adminDisableOfflineNotification()
	if err != nil {
		tgSend(chatID, "❌ 禁用失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "⏸ 离线通知已禁用", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlo"}}})
}

func handleTgAdminNotifyLoad(chatID int64) {
	data, err := adminListLoadAlerts()
	if err != nil {
		tgSend(chatID, "❌ 获取负载告警失败: "+err.Error())
		return
	}
	txt := fmtAdminNotifications(data, "负载告警配置")
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🔄 刷新", CallbackData: "adm_nll"}, {Text: "⬅️ 返回", CallbackData: "adm_no"}},
	})
}

func handleTgAdminNotifyTraffic(chatID int64) {
	data, err := adminListTrafficReports()
	if err != nil {
		tgSend(chatID, "❌ 获取流量报告失败: "+err.Error())
		return
	}
	txt := fmtAdminNotifications(data, "流量报告配置")
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "✅ 启用", CallbackData: "adm_nlet"}, {Text: "⏸ 禁用", CallbackData: "adm_nldt"}},
		{{Text: "🔄 刷新", CallbackData: "adm_nlt"}, {Text: "⬅️ 返回", CallbackData: "adm_no"}},
	})
}

func handleTgAdminNotifyEnableTraffic(chatID int64) {
	err := adminEnableTrafficReport()
	if err != nil {
		tgSend(chatID, "❌ 启用失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 流量报告已启用", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlt"}}})
}

func handleTgAdminNotifyDisableTraffic(chatID int64) {
	err := adminDisableTrafficReport()
	if err != nil {
		tgSend(chatID, "❌ 禁用失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "⏸ 流量报告已禁用", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlt"}}})
}

// Ping task management
func handleTgAdminPingTasks(chatID int64) {
	tasks, err := adminListPingTasks()
	if err != nil {
		tgSend(chatID, "❌ 获取Ping任务失败: "+err.Error())
		return
	}
	if len(tasks) == 0 {
		tgSendKB(chatID, "📡 暂无Ping任务", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm"}}})
		return
	}
	var s strings.Builder
	s.WriteString("📡 *Ping 任务管理*\n\n")
	for _, t := range tasks {
		status := "✅"
		if !t.DefaultOn {
			status = "⏸"
		}
		s.WriteString(fmt.Sprintf("%s %s (ID: %d)\n", status, t.Name, t.ID))
		s.WriteString(fmt.Sprintf("   类型: %s | 间隔: %ds\n", t.Type, t.Interval))
	}
	var btns [][]InlineButton
	for _, t := range tasks {
		btns = append(btns, []InlineButton{
			{Text: fmt.Sprintf("🗑 删除 #%d %s", t.ID, t.Name), CallbackData: fmt.Sprintf("adm_ptd:%d", t.ID)},
		})
		if len(btns) >= 10 {
			break
		}
	}
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: "adm_pt"}, {Text: "⬅️ 返回", CallbackData: "adm"}})
	tgSendKB(chatID, s.String(), btns)
}

func handleTgAdminPingTaskDeleteConfirm(chatID int64, idStr string) {
	var id int
	fmt.Sscanf(idStr, "%d", &id)
	tgSendKB(chatID, fmt.Sprintf("⚠️ *确认删除Ping任务 #%d?*\n\n此操作不可撤销!", id),
		[][]InlineButton{
			{{Text: "✅ 确认删除", CallbackData: fmt.Sprintf("adm_ptdy:%d", id)}, {Text: "❌ 取消", CallbackData: "adm_pt"}},
		})
}

func handleTgAdminPingTaskDelete(chatID int64, idStr string) {
	var id int
	fmt.Sscanf(idStr, "%d", &id)
	err := adminDeletePingTask(id)
	if err != nil {
		tgSend(chatID, "❌ 删除失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ Ping任务已删除", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_pt"}}})
}

// Remote task management
func handleTgAdminTasks(chatID int64) {
	data, err := adminListAllTasks()
	if err != nil {
		tgSend(chatID, "❌ 获取任务列表失败: "+err.Error())
		return
	}
	txt := fmtAdminTasks(data)
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🔄 刷新", CallbackData: "adm_tl"}, {Text: "⬅️ 返回", CallbackData: "adm"}},
	})
}

func handleTgAdminTaskDetail(chatID int64, taskID string) {
	data, err := adminGetTask(taskID)
	if err != nil {
		tgSend(chatID, "❌ 获取任务详情失败: "+err.Error())
		return
	}
	// Format as readable JSON
	var pretty interface{}
	if json.Unmarshal(data, &pretty) == nil {
		if formatted, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			txt := fmt.Sprintf("⚡ *任务详情*\n\n```\n%s\n```", string(formatted))
			if len(txt) > 4000 {
				txt = txt[:4000] + "...```"
			}
			tgSendKB(chatID, txt, [][]InlineButton{
				{{Text: "📊 结果", CallbackData: "adm_tdr:" + taskID}},
				{{Text: "⬅️ 返回", CallbackData: "adm_tl"}},
			})
			return
		}
	}
	tgSendKB(chatID, fmt.Sprintf("⚡ *任务详情*\n\n%s", string(data)),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_tl"}}})
}

func handleTgAdminTaskResult(chatID int64, taskID string) {
	data, err := adminGetTaskResult(taskID)
	if err != nil {
		tgSend(chatID, "❌ 获取任务结果失败: "+err.Error())
		return
	}
	var pretty interface{}
	if json.Unmarshal(data, &pretty) == nil {
		if formatted, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			txt := fmt.Sprintf("📊 *任务结果*\n\n```\n%s\n```", string(formatted))
			if len(txt) > 4000 {
				txt = txt[:4000] + "...```"
			}
			tgSendKB(chatID, txt, [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_td:" + taskID}}})
			return
		}
	}
	tgSendKB(chatID, fmt.Sprintf("📊 *任务结果*\n\n%s", string(data)),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_td:" + taskID}}})
}

// Audit logs
func handleTgAdminLogs(chatID int64, page int) {
	if page < 1 {
		page = 1
	}
	data, err := adminGetLogs(10, page)
	if err != nil {
		tgSend(chatID, "❌ 获取审计日志失败: "+err.Error())
		return
	}
	txt := fmtAdminLogs(data)
	var btns [][]InlineButton
	if page > 1 {
		btns = append(btns, []InlineButton{
			{Text: "⬅️ 上一页", CallbackData: fmt.Sprintf("adm_logp:%d", page-1)},
			{Text: "➡️ 下一页", CallbackData: fmt.Sprintf("adm_logp:%d", page+1)},
		})
	} else {
		btns = append(btns, []InlineButton{
			{Text: "➡️ 下一页", CallbackData: fmt.Sprintf("adm_logp:%d", page+1)},
		})
	}
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: fmt.Sprintf("adm_logp:%d", page)}, {Text: "⬅️ 返回", CallbackData: "adm"}})
	tgSendKB(chatID, txt, btns)
}

// Settings
func handleTgAdminSettings(chatID int64) {
	settings, err := adminGetSettings()
	if err != nil {
		tgSend(chatID, "❌ 获取设置失败: "+err.Error())
		return
	}
	txt := fmtAdminSettings(settings)
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🔄 刷新", CallbackData: "adm_set"}, {Text: "⬅️ 返回", CallbackData: "adm"}},
	})
}

// Sessions
func handleTgAdminSessions(chatID int64) {
	data, err := adminGetSessions()
	if err != nil {
		tgSend(chatID, "❌ 获取会话失败: "+err.Error())
		return
	}
	txt := fmtAdminSessions(data)
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "🗑 清除所有会话", CallbackData: "adm_sey"}},
		{{Text: "🔄 刷新", CallbackData: "adm_sess"}, {Text: "⬅️ 返回", CallbackData: "adm"}},
	})
}

func handleTgAdminRemoveAllSessionsConfirm(chatID int64) {
	tgSendKB(chatID, "⚠️ *确认清除所有会话?*\n\n这将使所有已登录的会话失效，包括当前bot的会话。",
		[][]InlineButton{
			{{Text: "✅ 确认", CallbackData: "adm_seyy"}, {Text: "❌ 取消", CallbackData: "adm_sess"}},
		})
}

func handleTgAdminRemoveAllSessions(chatID int64) {
	err := adminRemoveAllSessions()
	if err != nil {
		tgSend(chatID, "❌ 清除失败: "+err.Error())
		return
	}
	// Reset cached session token
	sessMu.Lock()
	sessToken = ""
	sessMu.Unlock()
	tgSendKB(chatID, "✅ 所有会话已清除", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm"}}})
}

// Record management
func handleTgAdminClearAllRecordsConfirm(chatID int64) {
	tgSendKB(chatID, "⚠️ *确认清空所有记录?*\n\n此操作不可撤销!",
		[][]InlineButton{
			{{Text: "✅ 确认清空", CallbackData: "adm_recy"}, {Text: "❌ 取消", CallbackData: "adm"}},
		})
}

func handleTgAdminClearAllRecords(chatID int64) {
	err := adminClearAllRecords()
	if err != nil {
		tgSend(chatID, "❌ 清空失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 所有记录已清空", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm"}}})
}
