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
	"sync"
)

// Telegram user state tracking for multi-step operations
var tgUserState sync.Map // chatID -> state string (e.g., "add_client", "edit_client:uuid")

func setUserState(chatID int64, state string) {
	tgUserState.Store(chatID, state)
}

func getUserState(chatID int64) string {
	if v, ok := tgUserState.Load(chatID); ok {
		return v.(string)
	}
	return ""
}

func clearUserState(chatID int64) {
	tgUserState.Delete(chatID)
}

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
	chatID := m.Chat.ID
	txt := strings.TrimSpace(m.Text)

	// Commands always take priority over state input
	if strings.HasPrefix(txt, "/") {
		clearUserState(chatID)
		handleTgCmd(chatID, strings.TrimPrefix(txt, "/"))
		return
	}

	// Check for pending user state (multi-step operations)
	state := getUserState(chatID)
	if state != "" {
		// Cancel/exit mechanism — clear state and notify user
		lower := strings.ToLower(txt)
		if lower == "取消" || lower == "cancel" || lower == "退出" || lower == "q" || lower == "exit" {
			clearUserState(chatID)
			tgSend(chatID, "🚫 已取消操作")
			return
		}
		clearUserState(chatID)
		switch {
		case state == "add_client":
			handleTgAdminClientAddSubmit(chatID, txt)
			return
		case strings.HasPrefix(state, "edit_client:"):
			// 如果用户输入的是 "edit xxx key=val" 或 "编辑 xxx key=val" 格式，走命令解析而非状态提交
			if strings.HasPrefix(txt, "edit ") || strings.HasPrefix(txt, "编辑 ") || strings.HasPrefix(txt, "客户端 编辑 ") {
				clearUserState(chatID)
				// fall through to normal command parsing below
			} else {
				uuid := strings.TrimPrefix(state, "edit_client:")
				handleTgAdminClientEditSubmit(chatID, uuid+"|"+txt)
				return
			}
		case state == "edit_offline_notify":
			handleTgAdminNotifyOfflineEditSubmit(chatID, txt)
			return
		case state == "add_load_alert":
			handleTgAdminLoadAlertAddSubmit(chatID, txt)
			return
		case strings.HasPrefix(state, "edit_load_alert:"):
			id := strings.TrimPrefix(state, "edit_load_alert:")
			handleTgAdminLoadAlertEditSubmit(chatID, id+"|"+txt)
			return
		case state == "edit_traffic_report":
			handleTgAdminTrafficReportEditSubmit(chatID, txt)
			return
		case state == "add_ping_task":
			handleTgAdminPingTaskAddSubmit(chatID, txt)
			return
		case strings.HasPrefix(state, "edit_ping_task:"):
			id := strings.TrimPrefix(state, "edit_ping_task:")
			handleTgAdminPingTaskEditSubmit(chatID, id+"|"+txt)
			return
		case state == "edit_settings":
			handleTgAdminSettingsEditSubmit(chatID, txt)
			return
		case state == "exec_command":
			handleTgAdminExecCommandSubmit(chatID, txt)
			return
		case state == "clear_records":
			handleTgAdminClearRecordsSubmit(chatID, txt)
			return
		}
	}

	// Telegram 文本命令：edit 节点名 key=value
	if strings.HasPrefix(txt, "edit ") || strings.HasPrefix(txt, "编辑 ") {
		prefix := "edit "
		if strings.HasPrefix(txt, "编辑 ") {
			prefix = "编辑 "
		}
		parts := strings.SplitN(strings.TrimSpace(txt[len(prefix):]), " ", 2)
		if len(parts) < 2 {
			tgSend(chatID, "❌ 格式错误\n\n格式: edit 节点名 key=value")
			return
		}
		name := parts[0]
		uuid, err := resolveNodeUUID(name)
		if err != nil {
			tgSend(chatID, "❌ "+err.Error())
			return
		}
		params := parseKeyValueParams(parts[1])
		if v, ok := params["weight"]; ok {
			params["weight"] = parseFloat(v.(string))
		}
		if v, ok := params["hidden"]; ok {
			params["hidden"] = v.(string) == "true"
		}
		if v, ok := params["price"]; ok {
			params["price"] = parseFloat(v.(string))
		}
		if v, ok := params["billing_cycle"]; ok {
			params["billing_cycle"] = parseInt(v.(string))
		}
		if v, ok := params["traffic_limit"]; ok {
			params["traffic_limit"] = parseUint64(v.(string))
		}
		err = adminEditClient(uuid, params)
		if err != nil {
			tgSend(chatID, "❌ 编辑失败: "+err.Error())
			return
		}
		tgSendKB(chatID, fmt.Sprintf("✅ 客户端 '%s' 已更新", name),
			[][]InlineButton{{{Text: "📋 查看详情", CallbackData: "adm_cd:" + uuid}}})
		return
	}

	ns := searchNodes(txt)
	switch len(ns) {
	case 1:
		handleTgNode(chatID, ns[0].UUID)
	case 0:
		tgSend(chatID, fmt.Sprintf("未找到: %s", txt))
	default:
		var s strings.Builder
		s.WriteString(fmt.Sprintf("找到 %d 个节点:\n", len(ns)))
		for i, n := range ns {
			s.WriteString(fmt.Sprintf("%d. %s\n", i+1, n.Name))
		}
		tgSend(chatID, s.String())
	}
}

func handleTgCmd(chatID int64, cmd string) {
	parts := strings.Fields(cmd)
	command := strings.ToLower(parts[0])
	// Strip @botname suffix (e.g. /help@mybot -> help)
	if idx := strings.Index(command, "@"); idx > 0 {
		command = command[:idx]
	}
	args := ""
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}
	switch command {
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
			"/new 名称 - 添加客户端\n"+
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
	case "new":
		handleTgCmdClientAdd(chatID, args)
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
		tgSend(chatID, "未知命令: /"+command)
	}
}

func handleTgCmdClientAdd(chatID int64, name string) {
	if name == "" {
		tgSend(chatID, "❌ 请提供客户端名称\n\n用法: /new 名称")
		return
	}
	params := map[string]interface{}{"name": name}
	err := adminAddClient(params)
	if err != nil {
		tgSend(chatID, "❌ 添加失败: "+err.Error())
		return
	}
	// Find the newly added client by name
	clients, err := adminListClients()
	if err != nil {
		tgSendKB(chatID, fmt.Sprintf("✅ 客户端 '%s' 已添加", name),
			[][]InlineButton{{{Text: "📋 查看列表", CallbackData: "adm_cl"}}})
		return
	}
	for _, c := range clients {
		if c.Name == name {
			// Show token and install command directly
			token, err := adminGetClientToken(c.UUID)
			if err != nil {
				tgSendKB(chatID, fmt.Sprintf("✅ 客户端 '%s' 已添加\n\nUUID: `%s`", name, c.UUID),
					[][]InlineButton{{{Text: "🔑 获取Token", CallbackData: "adm_ct:" + c.UUID}, {Text: "📋 查看列表", CallbackData: "adm_cl"}}})
				return
			}
			siteURL := strings.TrimRight(KomariUrl, "/")
			linuxURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.sh")
			winURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.ps1")
			linuxCmd := fmt.Sprintf("wget -qO- %s | sudo bash -s -- -e %s -t %s", linuxURL, siteURL, token)
			winCmd := fmt.Sprintf("powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"iwr '%s' -UseBasicParsing -OutFile 'install.ps1'; & '.\\install.ps1' '-e' '%s' '-t' '%s'\"", winURL, siteURL, token)
			msg := fmt.Sprintf("✅ 客户端 '%s' 已添加\n\nUUID: `%s`\nToken: `%s`\n\n", name, c.UUID, token)
			msg += "📦 *Linux 一键安装:*\n```\n" + linuxCmd + "\n```\n"
			msg += "📦 *Windows PowerShell:*\n```\n" + winCmd + "\n```\n"
			msg += "📦 *macOS / FreeBSD:*\n手动下载: [GitHub Releases](https://github.com/komari-monitor/komari-agent/releases)"
			tgSendKB(chatID, msg,
				[][]InlineButton{{{Text: "📋 查看列表", CallbackData: "adm_cl"}}})
			return
		}
	}
	tgSendKB(chatID, fmt.Sprintf("✅ 客户端 '%s' 已添加", name),
		[][]InlineButton{{{Text: "📋 查看列表", CallbackData: "adm_cl"}}})
}

func handleTgStatus(chatID int64) {
	nodes, err := getNodeList()
	if err != nil {
		tgSend(chatID, "❌ "+err.Error())
		return
	}
	statusMap := getAllNodeStatus()
	total := 0
	online := 0
	offline := 0
	var totalCPU float64
	var totalMemUsed, totalMemTotal uint64
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		total++
		if rt, ok := statusMap[n.UUID]; ok {
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
	// 优先用管理API获取完整信息（包含备注等字段）
	n, err := adminGetClient(uuid)
	if err != nil {
		// 管理API失败时回退到公共API
		n, err = getNodeByUUID(uuid)
		if err != nil {
			tgSend(chatID, "❌ "+err.Error())
			return
		}
	}
	rt, _ := getNodeRealtime(uuid)
	btns := [][]InlineButton{
		{{Text: "✏️ 编辑", CallbackData: "adm_ce:" + uuid}, {Text: "🔑 Token", CallbackData: "adm_ct:" + uuid}},
		{{Text: "📈 历史", CallbackData: "history:" + uuid}, {Text: "🔄 刷新", CallbackData: "node:" + uuid}},
		{{Text: "📋 返回列表", CallbackData: "cmd:list"}},
	}
	tgSendKB(chatID, fmtNodeStatus(n, rt), btns)
}

func handleTgCallback(cb *TgCallback) {
	answerCB(cb.ID)
	uid := cb.From.ID
	if !isUserAllowed(uid) {
		return
	}
	// Clear any pending user state when a button is clicked
	clearUserState(uid)
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
	case "adm_ca":
		handleTgAdminClientAddForm(uid)
	case "adm_cas":
		handleTgAdminClientAddSubmit(uid, param)
	case "adm_ce":
		handleTgAdminClientEditForm(uid, param)
	case "adm_ces":
		handleTgAdminClientEditSubmit(uid, param)
	case "adm_nloe":
		handleTgAdminNotifyOfflineEdit(uid)
	case "adm_nla":
		handleTgAdminLoadAlertAddForm(uid)
	case "adm_nlad":
		handleTgAdminLoadAlertAddSubmit(uid, param)
	case "adm_nld":
		handleTgAdminLoadAlertDeleteConfirm(uid, param)
	case "adm_nldy":
		handleTgAdminLoadAlertDelete(uid, param)
	case "adm_nle":
		handleTgAdminLoadAlertEditForm(uid, param)
	case "adm_nled":
		handleTgAdminLoadAlertEditSubmit(uid, param)
	case "adm_nlte":
		handleTgAdminTrafficReportEdit(uid)
	case "adm_pta":
		handleTgAdminPingTaskAddForm(uid)
	case "adm_ptad":
		handleTgAdminPingTaskAddSubmit(uid, param)
	case "adm_pte":
		handleTgAdminPingTaskEditForm(uid, param)
	case "adm_pted":
		handleTgAdminPingTaskEditSubmit(uid, param)
	case "adm_sete":
		handleTgAdminSettingsEditForm(uid)
	case "adm_seted":
		handleTgAdminSettingsEditSubmit(uid, param)
	case "adm_exec":
		handleTgAdminExecCommandForm(uid)
	case "adm_execd":
		handleTgAdminExecCommandSubmit(uid, param)
	case "adm_recd":
		handleTgAdminClearRecordsForm(uid)
	case "adm_recds":
		handleTgAdminClearRecordsSubmit(uid, param)
	case "adm_sessd":
		handleTgAdminRemoveSessionConfirm(uid, param)
	case "adm_sessdy":
		handleTgAdminRemoveSession(uid, param)
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
		var msg WecomCallbackXML
		if err := xml.Unmarshal(body, &msg); err != nil {
			logger.Printf("[WecomCallback] XML parse error: %v", err)
			w.Write([]byte("success"))
			return
		}
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
			var decMsg WecomCallbackXML
			xml.Unmarshal([]byte(decrypted), &decMsg)
			content = decMsg.Content
			msg.FromUserName = decMsg.FromUserName
			logger.Printf("[WecomCallback] Decrypted content: %s, from: %s", content, msg.FromUserName)
		}
		if content != "" {
			reply := processWecomMsg(content)
			logger.Printf("[WecomCallback] Reply (len=%d): %s", len(reply), reply[:min(200, len(reply))])
			if reply != "" {
				logger.Printf("[WecomCallback] Sending reply via API to %s", msg.FromUserName)
				// 异步发送回复，不阻塞回调响应
				go func(toUser, replyContent string) {
					token, err := getWecomAccessToken()
					if err != nil {
						logger.Printf("[WecomCallback] Get access token failed: %v", err)
						return
					}
					wd := WecomMsg{ToUser: toUser, MsgType: "text", AgentId: WecomAid}
					wd.Text.Content = replyContent
					resp, err := httpDo("POST", fmt.Sprintf(SendMsgURL, token), wd, nil)
					if err != nil {
						logger.Printf("[WecomCallback] Send failed: %v", err)
					} else {
						logger.Printf("[WecomCallback] Reply sent to %s, resp: %s", toUser, string(resp))
					}
				}(msg.FromUserName, reply)
			}
		}
		w.Write([]byte("success"))
	default:
		w.WriteHeader(405)
	}
}

func processWecomMsg(content string) string {
	content = strings.TrimSpace(content)

	// 精确匹配优先
	switch content {
	case "帮助", "help", "?", "菜单", "menu":
		return "📋 查询命令:\n" +
			"状态 - 节点状态概览\n" +
			"列表 - 所有节点列表\n" +
			"离线 - 离线节点\n" +
			"Ping - Ping任务\n" +
			"排名 - 性能排名\n" +
			"信息 - 站点信息\n" +
			"分组 - 节点分组\n\n" +
			"🔧 管理命令:\n" +
			"客户端 - 客户端管理\n" +
			"新 节点名 - 快速创建客户端\n" +
			"通知 - 通知管理\n" +
			"Ping任务 - Ping任务管理\n" +
			"远程任务 - 远程任务\n" +
			"日志 - 审计日志\n" +
			"会话 - 会话管理\n" +
			"设置 - 系统设置\n\n" +
			"📌 带参数命令:\n" +
			"详情 节点名 - 查看节点详情\n" +
			"Token 节点名 - 获取Token+安装命令\n" +
			"删除 节点名 - 删除客户端\n" +
			"添加 name=xxx region=xxx - 添加客户端\n" +
			"编辑 节点名 key=value - 编辑客户端\n\n" +
			"🔧 通知管理:\n" +
			"离线通知 编辑 key=value - 编辑离线通知\n" +
			"负载告警 添加 name=xxx type=cpu threshold=80\n" +
			"负载告警 删除 ID - 删除负载告警\n" +
			"负载告警 编辑 ID key=value - 编辑负载告警\n" +
			"流量报告 编辑 key=value - 编辑流量报告\n\n" +
			"📡 Ping管理:\n" +
			"Ping添加 name=xxx type=http target=xxx interval=60\n" +
			"Ping编辑 ID key=value - 编辑Ping任务\n\n" +
			"⚙️ 其他管理:\n" +
			"设置 编辑 key=value - 编辑设置\n" +
			"执行 命令 - 执行远程命令\n" +
			"清空记录 [type=load] - 清空记录\n" +
			"删除会话 [ID] - 删除会话\n\n" +
			"💡 可用参数 (英文):\n" +
			"name/region/group/tags/weight/hidden/price/currency/billing_cycle/public_remark/remark/traffic_limit/traffic_limit_type\n\n" +
			"直接输入节点名称快速查看"
	case "状态", "状态查询":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		statusMap := getAllNodeStatus()
		total := 0
		online := 0
		offline := 0
		var totalCPU float64
		var totalMemUsed, totalMemTotal uint64
		for _, n := range nodes {
			if n.Hidden {
				continue
			}
			total++
			if rt, ok := statusMap[n.UUID]; ok {
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
	case "列表":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtNodeListWithStatus(nodes)
	case "离线":
		return fmtOfflineNodes()
	case "Ping", "ping":
		return fmtPingInfo()
	case "排名":
		nodes, err := getNodeList()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtCPUUsageRank(nodes) + "\n\n" + fmtMemUsageRank(nodes)
	case "信息":
		return fmtSiteInfo()
	case "分组":
		return fmtGroupList()
	case "客户端":
		clients, err := adminListClients()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminClientList(clients)
	case "通知":
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
	case "Ping任务", "ping任务":
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
	case "远程任务":
		tasks, err := adminListAllTasks()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminTasks(tasks)
	case "日志":
		logs, err := adminGetLogs(20, 1)
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminLogs(logs)
	case "会话":
		sessions, err := adminGetSessions()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminSessions(sessions)
	case "设置":
		settings, err := adminGetSettings()
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmtAdminSettings(settings)
	case "清空记录":
		err := adminClearAllRecords()
		if err != nil {
			return "❌ 清空失败: " + err.Error()
		}
		return "✅ 所有记录已清空"
	}

	// 前缀匹配（带参数的命令）
	lower := strings.ToLower(content)

	// 详情 节点名
	if strings.HasPrefix(content, "详情 ") {
		name := strings.TrimSpace(content[len("详情 "):])
		ns := searchNodes(name)
		switch len(ns) {
		case 1:
			rt, _ := getNodeRealtime(ns[0].UUID)
			return fmtNodeStatus(&ns[0], rt)
		case 0:
			return fmt.Sprintf("未找到节点: %s", name)
		default:
			var s strings.Builder
			s.WriteString(fmt.Sprintf("找到 %d 个节点:\n", len(ns)))
			for i, n := range ns {
				s.WriteString(fmt.Sprintf("%d. %s\n", i+1, n.Name))
			}
			return s.String()
		}
	}

	// Token 节点名 (also accepts legacy "客户端 Token 节点名")
	if strings.HasPrefix(lower, "token ") || strings.HasPrefix(lower, "客户端 token ") || strings.HasPrefix(lower, "客户端 token	") {
		// find name after "token " or "客户端 token "
		var name string
		if strings.HasPrefix(lower, "token ") {
			name = strings.TrimSpace(content[len("Token "):])
		} else {
			name = strings.TrimSpace(content[len("客户端 Token "):])
			if name == "" {
				name = strings.TrimSpace(content[len("客户端 token "):])
			}
		}
		uuid, err := resolveNodeUUID(name)
		if err != nil {
			return "❌ " + err.Error()
		}
		token, err := adminGetClientToken(uuid)
		if err != nil {
			return "❌ 获取Token失败: " + err.Error()
		}
		siteURL := strings.TrimRight(KomariUrl, "/")
		linuxURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.sh")
		winURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.ps1")
		linuxCmd := fmt.Sprintf("wget -qO- %s | sudo bash -s -- -e %s -t %s", linuxURL, siteURL, token)
		winCmd := fmt.Sprintf("powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"iwr '%s' -UseBasicParsing -OutFile 'install.ps1'; & '.\\install.ps1' '-e' '%s' '-t' '%s'\"", winURL, siteURL, token)
		msg := fmt.Sprintf("🔑 客户端 Token\n\n名称: %s\nUUID: %s\nToken: %s\n\n", name, uuid, token)
		msg += "📦 Linux 一键安装:\n" + linuxCmd + "\n\n"
		msg += "📦 Windows PowerShell:\n" + winCmd + "\n\n"
		msg += "📦 macOS / FreeBSD:\n手动下载: https://github.com/komari-monitor/komari-agent/releases"
		return msg
	}

	// 删除 节点名 (also accepts legacy "客户端 删除 节点名")
	if strings.HasPrefix(content, "删除 ") || strings.HasPrefix(content, "客户端 删除 ") || strings.HasPrefix(content, "客户端 删除	") {
		var name string
		if strings.HasPrefix(content, "删除 ") {
			name = strings.TrimSpace(content[len("删除 "):])
		} else {
			name = strings.TrimSpace(content[len("客户端 删除 "):])
		}
		uuid, err := resolveNodeUUID(name)
		if err != nil {
			return "❌ " + err.Error()
		}
		err = adminRemoveClient(uuid)
		if err != nil {
			return "❌ 删除失败: " + err.Error()
		}
		return fmt.Sprintf("✅ 客户端 '%s' 已删除", name)
	}

	// 新 节点名 — 快速创建客户端
	if strings.HasPrefix(content, "新 ") {
		name := strings.TrimSpace(content[len("新 "):])
		if name == "" {
			return "❌ 请指定节点名\n\n格式: 新 节点名"
		}
		params := map[string]interface{}{"name": name}
		err := adminAddClient(params)
		if err != nil {
			return "❌ 添加失败: " + err.Error()
		}
		// 获取刚创建的客户端信息
		nodes, _ := getNodeList()
		var createdNode *KomariNode
		for _, n := range nodes {
			if n.Name == name {
				createdNode = &n
				break
			}
		}
		if createdNode == nil {
			return fmt.Sprintf("✅ 客户端 '%s' 已添加", name)
		}
		token, _ := adminGetClientToken(createdNode.UUID)
		siteURL := strings.TrimRight(KomariUrl, "/")
		linuxURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.sh")
		winURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.ps1")
		linuxCmd := fmt.Sprintf("wget -qO- %s | sudo bash -s -- -e %s -t %s", linuxURL, siteURL, token)
		winCmd := fmt.Sprintf("powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"iwr '%s' -UseBasicParsing -OutFile 'install.ps1'; & '.\\install.ps1' '-e' '%s' '-t' '%s'\"", winURL, siteURL, token)
		msg := fmt.Sprintf("✅ 客户端 '%s' 已添加\n\n", name)
		msg += fmt.Sprintf("UUID: %s\n", createdNode.UUID)
		if token != "" {
			msg += fmt.Sprintf("Token: %s\n\n", token)
			msg += "📦 Linux 一键安装:\n" + linuxCmd + "\n\n"
			msg += "📦 Windows PowerShell:\n" + winCmd + "\n\n"
			msg += "📦 macOS / FreeBSD:\n手动下载: https://github.com/komari-monitor/komari-agent/releases"
		}
		return msg
	}

	// 添加 name=xxx region=xxx ... (also accepts legacy "客户端 添加")
	if strings.HasPrefix(content, "添加 ") || strings.HasPrefix(content, "客户端 添加 ") || strings.HasPrefix(content, "客户端 添加	") {
		var paramStr string
		if strings.HasPrefix(content, "添加 ") {
			paramStr = strings.TrimSpace(content[len("添加 "):])
		} else {
			paramStr = strings.TrimSpace(content[len("客户端 添加 "):])
		}
		params := parseKeyValueParams(paramStr)
		if _, ok := params["name"]; !ok {
			return "❌ 缺少必填参数: name\n\n格式: 添加 name=节点名 region=区域 group=分组"
		}
		// Convert types
		if v, ok := params["weight"]; ok {
			params["weight"] = parseFloat(v.(string))
		}
		if v, ok := params["hidden"]; ok {
			params["hidden"] = v.(string) == "true"
		}
		if v, ok := params["price"]; ok {
			params["price"] = parseFloat(v.(string))
		}
		if v, ok := params["billing_cycle"]; ok {
			params["billing_cycle"] = parseInt(v.(string))
		}
		if v, ok := params["traffic_limit"]; ok {
			params["traffic_limit"] = parseUint64(v.(string))
		}
		err := adminAddClient(params)
		if err != nil {
			return "❌ 添加失败: " + err.Error()
		}
		// 获取刚创建的客户端信息
		name := params["name"].(string)
		nodes, _ := getNodeList()
		var createdNode *KomariNode
		for _, n := range nodes {
			if n.Name == name {
				createdNode = &n
				break
			}
		}
		if createdNode == nil {
			return fmt.Sprintf("✅ 客户端 '%s' 已添加", name)
		}
		token, _ := adminGetClientToken(createdNode.UUID)
		siteURL := strings.TrimRight(KomariUrl, "/")
		linuxURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.sh")
		winURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.ps1")
		linuxCmd := fmt.Sprintf("wget -qO- %s | sudo bash -s -- -e %s -t %s", linuxURL, siteURL, token)
		winCmd := fmt.Sprintf("powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"iwr '%s' -UseBasicParsing -OutFile 'install.ps1'; & '.\\install.ps1' '-e' '%s' '-t' '%s'\"", winURL, siteURL, token)
		msg := fmt.Sprintf("✅ 客户端 '%s' 已添加\n\n", name)
		msg += fmt.Sprintf("UUID: %s\n", createdNode.UUID)
		if token != "" {
			msg += fmt.Sprintf("Token: %s\n\n", token)
			msg += "📦 Linux 一键安装:\n" + linuxCmd + "\n\n"
			msg += "📦 Windows PowerShell:\n" + winCmd + "\n\n"
			msg += "📦 macOS / FreeBSD:\n手动下载: https://github.com/komari-monitor/komari-agent/releases"
		}
		return msg
	}

	// 编辑 节点名 key=value ... (also accepts legacy "客户端 编辑")
	if strings.HasPrefix(content, "编辑 ") || strings.HasPrefix(content, "客户端 编辑 ") || strings.HasPrefix(content, "客户端 编辑	") {
		var parts []string
		if strings.HasPrefix(content, "编辑 ") {
			parts = strings.SplitN(strings.TrimSpace(content[len("编辑 "):]), " ", 2)
		} else {
			parts = strings.SplitN(strings.TrimSpace(content[len("客户端 编辑 "):]), " ", 2)
		}
		if len(parts) < 2 {
			return "❌ 格式错误\n\n格式: 编辑 节点名 key=value key2=value2"
		}
		name := parts[0]
		uuid, err := resolveNodeUUID(name)
		if err != nil {
			return "❌ " + err.Error()
		}
		params := parseKeyValueParams(parts[1])
		// Convert types
		if v, ok := params["weight"]; ok {
			params["weight"] = parseFloat(v.(string))
		}
		if v, ok := params["hidden"]; ok {
			params["hidden"] = v.(string) == "true"
		}
		if v, ok := params["price"]; ok {
			params["price"] = parseFloat(v.(string))
		}
		if v, ok := params["billing_cycle"]; ok {
			params["billing_cycle"] = parseInt(v.(string))
		}
		if v, ok := params["traffic_limit"]; ok {
			params["traffic_limit"] = parseUint64(v.(string))
		}
		err = adminEditClient(uuid, params)
		if err != nil {
			return "❌ 编辑失败: " + err.Error()
		}
		return fmt.Sprintf("✅ 客户端 '%s' 已更新", name)
	}

	// 离线通知 编辑 key=value ...
	if strings.HasPrefix(content, "离线通知 编辑 ") || strings.HasPrefix(content, "离线通知 编辑	") {
		paramStr := strings.TrimSpace(content[len("离线通知 编辑 "):])
		params := parseKeyValueParams(paramStr)
		if v, ok := params["enabled"]; ok {
			params["enabled"] = v.(string) == "true"
		}
		if v, ok := params["interval"]; ok {
			params["interval"] = parseInt(v.(string))
		}
		err := adminEditOfflineNotification(params)
		if err != nil {
			return "❌ 编辑失败: " + err.Error()
		}
		return "✅ 离线通知配置已更新"
	}

	// 负载告警 添加 name=xxx type=cpu threshold=80
	if strings.HasPrefix(content, "负载告警 添加 ") || strings.HasPrefix(content, "负载告警 添加	") {
		paramStr := strings.TrimSpace(content[len("负载告警 添加 "):])
		params := parseKeyValueParams(paramStr)
		if _, ok := params["name"]; !ok {
			return "❌ 缺少必填参数: name\n\n格式: 负载告警 添加 name=规则名 type=cpu threshold=80"
		}
		if v, ok := params["threshold"]; ok {
			params["threshold"] = parseFloat(v.(string))
		}
		if v, ok := params["interval"]; ok {
			params["interval"] = parseInt(v.(string))
		}
		if v, ok := params["enabled"]; ok {
			params["enabled"] = v.(string) == "true"
		}
		err := adminAddLoadAlert(params)
		if err != nil {
			return "❌ 添加失败: " + err.Error()
		}
		return fmt.Sprintf("✅ 负载告警规则 '%s' 已添加", params["name"])
	}

	// 负载告警 删除 id
	if strings.HasPrefix(content, "负载告警 删除 ") || strings.HasPrefix(content, "负载告警 删除	") {
		idStr := strings.TrimSpace(content[len("负载告警 删除 "):])
		id := parseInt(idStr)
		if id == 0 {
			return "❌ 无效的ID"
		}
		err := adminDeleteLoadAlert(map[string]interface{}{"id": id})
		if err != nil {
			return "❌ 删除失败: " + err.Error()
		}
		return "✅ 负载告警规则已删除"
	}

	// 负载告警 编辑 id key=value ...
	if strings.HasPrefix(content, "负载告警 编辑 ") || strings.HasPrefix(content, "负载告警 编辑	") {
		parts := strings.SplitN(strings.TrimSpace(content[len("负载告警 编辑 "):]), " ", 2)
		if len(parts) < 2 {
			return "❌ 格式错误\n\n格式: 负载告警 编辑 ID key=value"
		}
		id := parseInt(parts[0])
		params := parseKeyValueParams(parts[1])
		params["id"] = id
		if v, ok := params["threshold"]; ok {
			params["threshold"] = parseFloat(v.(string))
		}
		if v, ok := params["interval"]; ok {
			params["interval"] = parseInt(v.(string))
		}
		if v, ok := params["enabled"]; ok {
			params["enabled"] = v.(string) == "true"
		}
		err := adminEditLoadAlert(params)
		if err != nil {
			return "❌ 编辑失败: " + err.Error()
		}
		return "✅ 负载告警规则已更新"
	}

	// 流量报告 编辑 key=value ...
	if strings.HasPrefix(content, "流量报告 编辑 ") || strings.HasPrefix(content, "流量报告 编辑	") {
		paramStr := strings.TrimSpace(content[len("流量报告 编辑 "):])
		params := parseKeyValueParams(paramStr)
		if v, ok := params["enabled"]; ok {
			params["enabled"] = v.(string) == "true"
		}
		if v, ok := params["interval"]; ok {
			params["interval"] = parseInt(v.(string))
		}
		err := adminEditTrafficReport(params)
		if err != nil {
			return "❌ 编辑失败: " + err.Error()
		}
		return "✅ 流量报告配置已更新"
	}

	// Ping添加 name=xxx type=http target=xxx interval=60
	if strings.HasPrefix(content, "Ping添加 ") || strings.HasPrefix(content, "ping添加 ") {
		paramStr := strings.TrimSpace(content[len("Ping添加 "):])
		if strings.HasPrefix(lower, "ping添加 ") {
			paramStr = strings.TrimSpace(content[len("ping添加 "):])
		}
		params := parseKeyValueParams(paramStr)
		if _, ok := params["name"]; !ok {
			return "❌ 缺少必填参数: name\n\n格式: Ping添加 name=任务名 type=http target=https://example.com interval=60"
		}
		if v, ok := params["interval"]; ok {
			params["interval"] = parseInt(v.(string))
		}
		if v, ok := params["default_on"]; ok {
			params["default_on"] = v.(string) == "true"
		}
		if v, ok := params["clients"]; ok {
			params["clients"] = strings.Split(v.(string), ",")
		}
		err := adminAddPingTask(params)
		if err != nil {
			return "❌ 添加失败: " + err.Error()
		}
		return fmt.Sprintf("✅ Ping任务 '%s' 已添加", params["name"])
	}

	// Ping编辑 id key=value ...
	if strings.HasPrefix(content, "Ping编辑 ") || strings.HasPrefix(content, "ping编辑 ") {
		paramStr := strings.TrimSpace(content[len("Ping编辑 "):])
		if strings.HasPrefix(lower, "ping编辑 ") {
			paramStr = strings.TrimSpace(content[len("ping编辑 "):])
		}
		parts := strings.SplitN(paramStr, " ", 2)
		if len(parts) < 2 {
			return "❌ 格式错误\n\n格式: Ping编辑 ID key=value"
		}
		id := parseInt(parts[0])
		params := parseKeyValueParams(parts[1])
		if v, ok := params["interval"]; ok {
			params["interval"] = parseInt(v.(string))
		}
		if v, ok := params["default_on"]; ok {
			params["default_on"] = v.(string) == "true"
		}
		if v, ok := params["clients"]; ok {
			params["clients"] = strings.Split(v.(string), ",")
		}
		err := adminEditPingTask(id, params)
		if err != nil {
			return "❌ 编辑失败: " + err.Error()
		}
		return "✅ Ping任务已更新"
	}

	// 设置 编辑 key=value ...
	if strings.HasPrefix(content, "设置 编辑 ") || strings.HasPrefix(content, "设置 编辑	") {
		paramStr := strings.TrimSpace(content[len("设置 编辑 "):])
		params := parseKeyValueParams(paramStr)
		if v, ok := params["record_enabled"]; ok {
			params["record_enabled"] = v.(string) == "true"
		}
		if v, ok := params["private_site"]; ok {
			params["private_site"] = v.(string) == "true"
		}
		if v, ok := params["allow_guest_view_node"]; ok {
			params["allow_guest_view_node"] = v.(string) == "true"
		}
		if v, ok := params["allow_guest_view_stats"]; ok {
			params["allow_guest_view_stats"] = v.(string) == "true"
		}
		if v, ok := params["record_preserve_time"]; ok {
			params["record_preserve_time"] = parseInt(v.(string))
		}
		err := adminEditSettings(params)
		if err != nil {
			return "❌ 编辑失败: " + err.Error()
		}
		return "✅ 设置已更新"
	}

	// 执行 command
	if strings.HasPrefix(content, "执行 ") || strings.HasPrefix(content, "执行	") {
		command := strings.TrimSpace(content[len("执行 "):])
		if command == "" {
			return "❌ 命令不能为空\n\n格式: 执行 ls -la"
		}
		result, err := adminExecCommand(command)
		if err != nil {
			return "❌ 执行失败: " + err.Error()
		}
		return fmt.Sprintf("⚡ 命令执行结果\n\n命令: %s\n\n%s", command, string(result))
	}

	// 清空记录 type=load before=2024-01-01
	if strings.HasPrefix(content, "清空记录 ") || strings.HasPrefix(content, "清空记录	") {
		paramStr := strings.TrimSpace(content[len("清空记录 "):])
		if paramStr == "" || paramStr == "全部" {
			err := adminClearAllRecords()
			if err != nil {
				return "❌ 清空失败: " + err.Error()
			}
			return "✅ 所有记录已清空"
		}
		params := parseKeyValueParams(paramStr)
		err := adminClearRecords(params)
		if err != nil {
			return "❌ 清空失败: " + err.Error()
		}
		return "✅ 记录已清空"
	}

	// 删除会话 id
	if strings.HasPrefix(content, "删除会话 ") || strings.HasPrefix(content, "删除会话	") {
		id := strings.TrimSpace(content[len("删除会话 "):])
		if id == "" || id == "全部" {
			err := adminRemoveAllSessions()
			if err != nil {
				return "❌ 删除失败: " + err.Error()
			}
			sessMu.Lock()
			sessToken = ""
			sessMu.Unlock()
			return "✅ 所有会话已删除"
		}
		err := adminRemoveSession(id)
		if err != nil {
			return "❌ 删除失败: " + err.Error()
		}
		return fmt.Sprintf("✅ 会话 '%s' 已删除", id)
	}

	// 默认：尝试匹配节点名
	ns := searchNodes(content)
	switch len(ns) {
	case 1:
		rt, _ := getNodeRealtime(ns[0].UUID)
		return fmtNodeStatus(&ns[0], rt)
	case 0:
		return fmt.Sprintf("未找到: %s\n\n发送 帮助 查看可用命令", content)
	default:
		var s strings.Builder
		s.WriteString(fmt.Sprintf("找到 %d 个节点:\n", len(ns)))
		for i, n := range ns {
			s.WriteString(fmt.Sprintf("%d. %s\n", i+1, n.Name))
		}
		return s.String()
	}
}

// resolveNodeUUID 根据节点名称查找UUID
func resolveNodeUUID(name string) (string, error) {
	ns := searchNodes(name)
	switch len(ns) {
	case 0:
		return "", fmt.Errorf("未找到节点: %s", name)
	case 1:
		return ns[0].UUID, nil
	default:
		var s strings.Builder
		s.WriteString(fmt.Sprintf("找到 %d 个节点，请用更精确的名称:\n", len(ns)))
		for i, n := range ns {
			s.WriteString(fmt.Sprintf("%d. %s\n", i+1, n.Name))
		}
		return "", fmt.Errorf("%s", s.String())
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
	btns = append(btns, []InlineButton{{Text: "➕ 添加客户端", CallbackData: "adm_ca"}})
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
		{{Text: "✏️ 编辑", CallbackData: "adm_ce:" + uuid}, {Text: "🔑 Token", CallbackData: "adm_ct:" + uuid}},
		{{Text: "🗑 删除", CallbackData: "adm_crm:" + uuid}},
		{{Text: "🔄 刷新", CallbackData: "adm_cd:" + uuid}, {Text: "📋 返回列表", CallbackData: "adm_cl"}},
	})
}

func handleTgAdminClientToken(chatID int64, uuid string) {
	token, err := adminGetClientToken(uuid)
	if err != nil {
		tgSend(chatID, "❌ 获取Token失败: "+err.Error())
		return
	}
	siteURL := strings.TrimRight(KomariUrl, "/")
	linuxURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.sh")
	winURL := ghURL("https://raw.githubusercontent.com/komari-monitor/komari-agent/refs/heads/main/install.ps1")
	linuxCmd := fmt.Sprintf("wget -qO- %s | sudo bash -s -- -e %s -t %s", linuxURL, siteURL, token)
	winCmd := fmt.Sprintf("powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"iwr '%s' -UseBasicParsing -OutFile 'install.ps1'; & '.\\install.ps1' '-e' '%s' '-t' '%s'\"", winURL, siteURL, token)
	msg := fmt.Sprintf("🔑 *客户端 Token*\n\nUUID: `%s`\nToken: `%s`\n\n", uuid, token)
	msg += "📦 *Linux 一键安装:*\n```\n" + linuxCmd + "\n```\n"
	msg += "📦 *Windows PowerShell:*\n```\n" + winCmd + "\n```\n"
	msg += "📦 *macOS / FreeBSD:*\n手动下载: [GitHub Releases](https://github.com/komari-monitor/komari-agent/releases)"
	tgSendKB(chatID, msg,
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
		{{Text: "✏️ 编辑配置", CallbackData: "adm_nloe"}},
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

	// Try to parse alerts for edit/delete buttons
	var btns [][]InlineButton
	var alerts []AdminNotificationConfig
	if json.Unmarshal(data, &alerts) == nil {
		for i, a := range alerts {
			btns = append(btns, []InlineButton{
				{Text: fmt.Sprintf("✏️ %s", a.Name), CallbackData: fmt.Sprintf("adm_nle:%d", a.ID)},
				{Text: "🗑", CallbackData: fmt.Sprintf("adm_nld:%d", a.ID)},
			})
			if i >= 8 {
				break
			}
		}
	}
	btns = append(btns, []InlineButton{{Text: "➕ 添加规则", CallbackData: "adm_nla"}})
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: "adm_nll"}, {Text: "⬅️ 返回", CallbackData: "adm_no"}})
	tgSendKB(chatID, txt, btns)
}

func handleTgAdminNotifyTraffic(chatID int64) {
	data, err := adminListTrafficReports()
	if err != nil {
		tgSend(chatID, "❌ 获取流量报告失败: "+err.Error())
		return
	}
	txt := fmtAdminNotifications(data, "流量报告配置")
	tgSendKB(chatID, txt, [][]InlineButton{
		{{Text: "✏️ 编辑配置", CallbackData: "adm_nlte"}},
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
			{Text: fmt.Sprintf("✏️ #%d %s", t.ID, t.Name), CallbackData: fmt.Sprintf("adm_pte:%d", t.ID)},
			{Text: "🗑", CallbackData: fmt.Sprintf("adm_ptd:%d", t.ID)},
		})
		if len(btns) >= 10 {
			break
		}
	}
	btns = append(btns, []InlineButton{{Text: "➕ 添加任务", CallbackData: "adm_pta"}})
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
		{{Text: "⚡ 执行命令", CallbackData: "adm_exec"}},
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
		{{Text: "✏️ 编辑设置", CallbackData: "adm_sete"}},
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

	var btns [][]InlineButton
	// Try to parse sessions for delete buttons
	var sessions []AdminSession
	if json.Unmarshal(data, &sessions) == nil {
		for i, sess := range sessions {
			if i >= 8 {
				break
			}
			name := sess.Username
			if name == "" {
				name = fmt.Sprintf("User#%d", sess.UserID)
			}
			btns = append(btns, []InlineButton{
				{Text: fmt.Sprintf("🗑 %s", name), CallbackData: fmt.Sprintf("adm_sessd:%s", sess.ID)},
			})
		}
	}
	btns = append(btns, []InlineButton{{Text: "🗑 清除所有会话", CallbackData: "adm_sey"}})
	btns = append(btns, []InlineButton{{Text: "🔄 刷新", CallbackData: "adm_sess"}, {Text: "⬅️ 返回", CallbackData: "adm"}})
	tgSendKB(chatID, txt, btns)
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

// ============================================================
// 1. Client Management - Add Client
// ============================================================

func handleTgAdminClientAddForm(chatID int64) {
	setUserState(chatID, "add_client")
	tgSendKB(chatID, "➕ *Add Client*\n\n"+
		"📝 *Send parameters in this format:*\n"+
		"```\nname=NodeName\nregion=🇨🇳\ngroup=home\nweight=0\nhidden=false\n```\n\n"+
		"✅ *Required:* name\n"+
		"💡 *Optional:* region, group, tags, weight, hidden, price, currency, billing_cycle, public_remark, remark, traffic_limit, traffic_limit_type\n\n"+
		"⚠️ Only fill in the params you want to set, others use default\n\n💡 Type `cancel` to exit",
		[][]InlineButton{{{Text: "❌ Cancel", CallbackData: "adm_cl"}}})
}

func handleTgAdminClientAddSubmit(chatID int64, param string) {
	params := parseKeyValueParams(param)
	if _, ok := params["name"]; !ok {
		tgSend(chatID, "❌ 缺少必填参数: 名称")
		return
	}
	// Convert types
	if v, ok := params["weight"]; ok {
		params["weight"] = parseFloat(v.(string))
	}
	if v, ok := params["hidden"]; ok {
		params["hidden"] = v.(string) == "true"
	}
	if v, ok := params["price"]; ok {
		params["price"] = parseFloat(v.(string))
	}
	if v, ok := params["billing_cycle"]; ok {
		params["billing_cycle"] = parseInt(v.(string))
	}
	if v, ok := params["traffic_limit"]; ok {
		params["traffic_limit"] = parseUint64(v.(string))
	}
	if v, ok := params["auto_renewal"]; ok {
		params["auto_renewal"] = v.(string) == "true"
	}

	err := adminAddClient(params)
	if err != nil {
		tgSend(chatID, "❌ 添加失败: "+err.Error())
		return
	}
	tgSendKB(chatID, fmt.Sprintf("✅ 客户端 '%s' 已添加", params["name"]),
		[][]InlineButton{{{Text: "📋 返回列表", CallbackData: "adm_cl"}}})
}

// ============================================================
// 2. Client Management - Edit Client
// ============================================================

func handleTgAdminClientEditForm(chatID int64, uuid string) {
	setUserState(chatID, "edit_client:"+uuid)
	client, err := adminGetClient(uuid)
	if err != nil {
		tgSend(chatID, "❌ 获取客户端失败: "+err.Error())
		return
	}
	short := uuid
	if len(uuid) > 8 {
		short = uuid[:8]
	}
	tgSendKB(chatID, fmt.Sprintf("✏️ *Edit Client: %s*\n\n"+
		"📋 Current Info:\n"+
		"  name: %s\n  region: %s\n  group: %s\n  weight: %d\n  hidden: %v\n"+
		"  tags: %s\n  public_remark: %s\n  remark: %s\n\n"+
		"💡 Click a button below to copy the command, change the value after = and send\n"+
		"💡 Or directly type `name=NewName` to modify\n\n"+
		"⚠️ Type `cancel` to exit",
		client.Name, client.Name, client.Region, client.Group, client.Weight, client.Hidden,
		client.Tags, client.PublicRemark, client.Remark),
		[][]InlineButton{
			{{Text: "📝 name: " + client.Name, SwitchInlineQueryCurrentChat: "edit " + short + " name="}},
			{{Text: "🌍 region: " + client.Region, SwitchInlineQueryCurrentChat: "edit " + short + " region="}},
			{{Text: "📂 group: " + client.Group, SwitchInlineQueryCurrentChat: "edit " + short + " group="}},
			{{Text: "⚖️ weight: " + fmt.Sprintf("%d", client.Weight), SwitchInlineQueryCurrentChat: "edit " + short + " weight="}},
			{{Text: "🏷️ public_remark: " + client.PublicRemark, SwitchInlineQueryCurrentChat: "edit " + short + " public_remark="}},
			{{Text: "🔒 remark: " + client.Remark, SwitchInlineQueryCurrentChat: "edit " + short + " remark="}},
			{{Text: "⬅️ Back", CallbackData: "adm_cd:" + uuid}},
		})
}

func handleTgAdminClientEditSubmit(chatID int64, param string) {
	// param format: uuid|key1=val1\nkey2=val2
	parts := strings.SplitN(param, "|", 2)
	if len(parts) < 2 {
		tgSend(chatID, "❌ 参数格式错误")
		return
	}
	uuid := parts[0]
	params := parseKeyValueParams(parts[1])

	// Convert types
	if v, ok := params["weight"]; ok {
		params["weight"] = parseFloat(v.(string))
	}
	if v, ok := params["hidden"]; ok {
		params["hidden"] = v.(string) == "true"
	}
	if v, ok := params["price"]; ok {
		params["price"] = parseFloat(v.(string))
	}
	if v, ok := params["billing_cycle"]; ok {
		params["billing_cycle"] = parseInt(v.(string))
	}
	if v, ok := params["traffic_limit"]; ok {
		params["traffic_limit"] = parseUint64(v.(string))
	}
	if v, ok := params["auto_renewal"]; ok {
		params["auto_renewal"] = v.(string) == "true"
	}

	err := adminEditClient(uuid, params)
	if err != nil {
		tgSend(chatID, "❌ 编辑失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 客户端已更新",
		[][]InlineButton{{{Text: "📋 返回详情", CallbackData: "adm_cd:" + uuid}}})
}

// ============================================================
// 3. Notification - Edit Offline Notification
// ============================================================

func handleTgAdminNotifyOfflineEdit(chatID int64) {
	setUserState(chatID, "edit_offline_notify")
	tgSendKB(chatID, "✏️ *编辑离线通知配置*\n\n"+
		"请按以下格式发送参数:\n\n"+
		"```\n"+
		"enabled=true\n"+
		"interval=300\n"+
		"```\n\n"+
		"可修改参数: enabled, interval, type\n\n💡 随时输入 `取消` 退出",
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlo"}}})
}

func handleTgAdminNotifyOfflineEditSubmit(chatID int64, param string) {
	params := parseKeyValueParams(param)
	if v, ok := params["enabled"]; ok {
		params["enabled"] = v.(string) == "true"
	}
	if v, ok := params["interval"]; ok {
		params["interval"] = parseInt(v.(string))
	}
	err := adminEditOfflineNotification(params)
	if err != nil {
		tgSend(chatID, "❌ 编辑失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 离线通知配置已更新", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlo"}}})
}

// ============================================================
// 4. Notification - Load Alert Add
// ============================================================

func handleTgAdminLoadAlertAddForm(chatID int64) {
	setUserState(chatID, "add_load_alert")
	tgSendKB(chatID, "➕ *添加负载告警规则*\n\n"+
		"请按以下格式发送参数:\n\n"+
		"```\n"+
		"name=规则名称\n"+
		"type=cpu\n"+
		"threshold=80\n"+
		"interval=300\n"+
		"enabled=true\n"+
		"```\n\n"+
		"type可选: cpu, memory, disk, network, load\n"+
		"threshold: 告警阈值\n\n💡 随时输入 `取消` 退出",
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nll"}}})
}

func handleTgAdminLoadAlertAddSubmit(chatID int64, param string) {
	params := parseKeyValueParams(param)
	if _, ok := params["name"]; !ok {
		tgSend(chatID, "❌ 缺少必填参数: name")
		return
	}
	if v, ok := params["threshold"]; ok {
		params["threshold"] = parseFloat(v.(string))
	}
	if v, ok := params["interval"]; ok {
		params["interval"] = parseInt(v.(string))
	}
	if v, ok := params["enabled"]; ok {
		params["enabled"] = v.(string) == "true"
	}

	err := adminAddLoadAlert(params)
	if err != nil {
		tgSend(chatID, "❌ 添加失败: "+err.Error())
		return
	}
	tgSendKB(chatID, fmt.Sprintf("✅ 负载告警规则 '%s' 已添加", params["name"]),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nll"}}})
}

// ============================================================
// 5. Notification - Load Alert Delete
// ============================================================

func handleTgAdminLoadAlertDeleteConfirm(chatID int64, idStr string) {
	tgSendKB(chatID, fmt.Sprintf("⚠️ *确认删除负载告警规则 #%s?*\n\n此操作不可撤销!", idStr),
		[][]InlineButton{
			{{Text: "✅ 确认删除", CallbackData: "adm_nldy:" + idStr}, {Text: "❌ 取消", CallbackData: "adm_nll"}},
		})
}

func handleTgAdminLoadAlertDelete(chatID int64, idStr string) {
	id := parseInt(idStr)
	err := adminDeleteLoadAlert(map[string]interface{}{"id": id})
	if err != nil {
		tgSend(chatID, "❌ 删除失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 负载告警规则已删除", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nll"}}})
}

// ============================================================
// 6. Notification - Load Alert Edit
// ============================================================

func handleTgAdminLoadAlertEditForm(chatID int64, idStr string) {
	setUserState(chatID, "edit_load_alert:"+idStr)
	tgSendKB(chatID, fmt.Sprintf("✏️ *编辑负载告警规则 #%s*\n\n"+
		"请按以下格式发送要修改的参数:\n\n"+
		"```\n"+
		"name=规则名称\n"+
		"threshold=80\n"+
		"interval=300\n"+
		"enabled=true\n"+
		"```", idStr),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nll"}}})
}

func handleTgAdminLoadAlertEditSubmit(chatID int64, param string) {
	parts := strings.SplitN(param, "|", 2)
	if len(parts) < 2 {
		tgSend(chatID, "❌ 参数格式错误")
		return
	}
	id := parseInt(parts[0])
	params := parseKeyValueParams(parts[1])
	params["id"] = id

	if v, ok := params["threshold"]; ok {
		params["threshold"] = parseFloat(v.(string))
	}
	if v, ok := params["interval"]; ok {
		params["interval"] = parseInt(v.(string))
	}
	if v, ok := params["enabled"]; ok {
		params["enabled"] = v.(string) == "true"
	}

	err := adminEditLoadAlert(params)
	if err != nil {
		tgSend(chatID, "❌ 编辑失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 负载告警规则已更新", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nll"}}})
}

// ============================================================
// 7. Notification - Traffic Report Edit
// ============================================================

func handleTgAdminTrafficReportEdit(chatID int64) {
	setUserState(chatID, "edit_traffic_report")
	tgSendKB(chatID, "✏️ *编辑流量报告配置*\n\n"+
		"请按以下格式发送参数:\n\n"+
		"```\n"+
		"enabled=true\n"+
		"interval=86400\n"+
		"type=daily\n"+
		"```\n\n"+
		"type可选: daily, weekly, monthly\n\n💡 随时输入 `取消` 退出",
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlt"}}})
}

func handleTgAdminTrafficReportEditSubmit(chatID int64, param string) {
	params := parseKeyValueParams(param)
	if v, ok := params["enabled"]; ok {
		params["enabled"] = v.(string) == "true"
	}
	if v, ok := params["interval"]; ok {
		params["interval"] = parseInt(v.(string))
	}
	err := adminEditTrafficReport(params)
	if err != nil {
		tgSend(chatID, "❌ 编辑失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 流量报告配置已更新", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_nlt"}}})
}

// ============================================================
// 8. Ping Task - Add
// ============================================================

func handleTgAdminPingTaskAddForm(chatID int64) {
	setUserState(chatID, "add_ping_task")
	tgSendKB(chatID, "➕ *添加Ping任务*\n\n"+
		"请按以下格式发送参数:\n\n"+
		"```\n"+
		"name=任务名称\n"+
		"type=http\n"+
		"target=https://example.com\n"+
		"interval=60\n"+
		"default_on=true\n"+
		"```\n\n"+
		"type可选: http, tcp, ping\n"+
		"clients=client1,client2 (可选，逗号分隔)\n\n💡 随时输入 `取消` 退出",
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_pt"}}})
}

func handleTgAdminPingTaskAddSubmit(chatID int64, param string) {
	params := parseKeyValueParams(param)
	if _, ok := params["name"]; !ok {
		tgSend(chatID, "❌ 缺少必填参数: name")
		return
	}
	if v, ok := params["interval"]; ok {
		params["interval"] = parseInt(v.(string))
	}
	if v, ok := params["default_on"]; ok {
		params["default_on"] = v.(string) == "true"
	}
	if v, ok := params["clients"]; ok {
		params["clients"] = strings.Split(v.(string), ",")
	}

	err := adminAddPingTask(params)
	if err != nil {
		tgSend(chatID, "❌ 添加失败: "+err.Error())
		return
	}
	tgSendKB(chatID, fmt.Sprintf("✅ Ping任务 '%s' 已添加", params["name"]),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_pt"}}})
}

// ============================================================
// 9. Ping Task - Edit
// ============================================================

func handleTgAdminPingTaskEditForm(chatID int64, idStr string) {
	setUserState(chatID, "edit_ping_task:"+idStr)
	tgSendKB(chatID, fmt.Sprintf("✏️ *编辑Ping任务 #%s*\n\n"+
		"请按以下格式发送要修改的参数:\n\n"+
		"```\n"+
		"name=任务名称\n"+
		"interval=60\n"+
		"default_on=true\n"+
		"```", idStr),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_pt"}}})
}

func handleTgAdminPingTaskEditSubmit(chatID int64, param string) {
	parts := strings.SplitN(param, "|", 2)
	if len(parts) < 2 {
		tgSend(chatID, "❌ 参数格式错误")
		return
	}
	id := parseInt(parts[0])
	params := parseKeyValueParams(parts[1])

	if v, ok := params["interval"]; ok {
		params["interval"] = parseInt(v.(string))
	}
	if v, ok := params["default_on"]; ok {
		params["default_on"] = v.(string) == "true"
	}
	if v, ok := params["clients"]; ok {
		params["clients"] = strings.Split(v.(string), ",")
	}

	err := adminEditPingTask(id, params)
	if err != nil {
		tgSend(chatID, "❌ 编辑失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ Ping任务已更新", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_pt"}}})
}

// ============================================================
// 10. Settings - Edit
// ============================================================

func handleTgAdminSettingsEditForm(chatID int64) {
	setUserState(chatID, "edit_settings")
	settings, err := adminGetSettings()
	if err != nil {
		tgSend(chatID, "❌ 获取设置失败: "+err.Error())
		return
	}
	current := fmtAdminSettings(settings)
	tgSendKB(chatID, current+"\n\n"+
		"✏️ *编辑设置*\n\n"+
		"请按以下格式发送要修改的参数:\n\n"+
		"```\n"+
		"sitename=站点名称\n"+
		"description=站点描述\n"+
		"theme=default\n"+
		"record_enabled=true\n"+
		"private_site=false\n"+
		"allow_guest_view_node=true\n"+
		"allow_guest_view_stats=true\n"+
		"record_preserve_time=7\n" +
				"```\n\n💡 随时输入 `取消` 退出",
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_set"}}})
}

func handleTgAdminSettingsEditSubmit(chatID int64, param string) {
	params := parseKeyValueParams(param)
	// Convert types
	if v, ok := params["record_enabled"]; ok {
		params["record_enabled"] = v.(string) == "true"
	}
	if v, ok := params["private_site"]; ok {
		params["private_site"] = v.(string) == "true"
	}
	if v, ok := params["allow_guest_view_node"]; ok {
		params["allow_guest_view_node"] = v.(string) == "true"
	}
	if v, ok := params["allow_guest_view_stats"]; ok {
		params["allow_guest_view_stats"] = v.(string) == "true"
	}
	if v, ok := params["record_preserve_time"]; ok {
		params["record_preserve_time"] = parseInt(v.(string))
	}

	err := adminEditSettings(params)
	if err != nil {
		tgSend(chatID, "❌ 编辑失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 设置已更新", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_set"}}})
}

// ============================================================
// 11. Remote Tasks - Execute Command
// ============================================================

func handleTgAdminExecCommandForm(chatID int64) {
	setUserState(chatID, "exec_command")
	tgSendKB(chatID, "⚡ *执行远程命令*\n\n"+
		"⚠️ 警告: 此命令将在所有客户端上执行!\n\n"+
		"请输入要执行的命令:\n\n💡 随时输入 `取消` 退出",
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_tl"}}})
}

func handleTgAdminExecCommandSubmit(chatID int64, command string) {
	if command == "" {
		tgSend(chatID, "❌ 命令不能为空")
		return
	}
	result, err := adminExecCommand(command)
	if err != nil {
		tgSend(chatID, "❌ 执行失败: "+err.Error())
		return
	}
	// Format result
	var pretty interface{}
	if json.Unmarshal(result, &pretty) == nil {
		if formatted, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			txt := fmt.Sprintf("⚡ *命令执行结果*\n\n命令: `%s`\n\n```\\n%s\\n```", command, string(formatted))
			if len(txt) > 4000 {
				txt = txt[:4000] + "...```"
			}
			tgSendKB(chatID, txt, [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_tl"}}})
			return
		}
	}
	tgSendKB(chatID, fmt.Sprintf("⚡ *命令执行结果*\n\n命令: `%s`\n\n%s", command, string(result)),
		[][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_tl"}}})
}

// ============================================================
// 12. Record Management - Clear Specific Records
// ============================================================

func handleTgAdminClearRecordsForm(chatID int64) {
	setUserState(chatID, "clear_records")
	tgSendKB(chatID, "🗑 *清空特定记录*\n\n"+
		"请按以下格式发送参数:\n\n"+
		"```\n"+
		"type=load\n"+
		"client=uuid\n"+
		"before=2024-01-01\n"+
		"```\n\n"+
		"type可选: load, ping, task\n"+
		"client: 可选，指定客户端UUID\n"+
		"before: 可选，清除此日期之前的记录\n\n💡 随时输入 `取消` 退出",
		[][]InlineButton{
			{{Text: "🗑 清空所有记录", CallbackData: "adm_rec"}},
			{{Text: "⬅️ 返回", CallbackData: "adm"}},
		})
}

func handleTgAdminClearRecordsSubmit(chatID int64, param string) {
	params := parseKeyValueParams(param)
	err := adminClearRecords(params)
	if err != nil {
		tgSend(chatID, "❌ 清空失败: "+err.Error())
		return
	}
	tgSendKB(chatID, "✅ 记录已清空", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm"}}})
}

// ============================================================
// 13. Session Management - Remove Specific Session
// ============================================================

func handleTgAdminRemoveSessionConfirm(chatID int64, id string) {
	tgSendKB(chatID, fmt.Sprintf("⚠️ *确认删除会话 #%s?*\n\n此操作将使该会话失效!", id),
		[][]InlineButton{
			{{Text: "✅ 确认删除", CallbackData: "adm_sessdy:" + id}, {Text: "❌ 取消", CallbackData: "adm_sess"}},
		})
}

func handleTgAdminRemoveSession(chatID int64, id string) {
	err := adminRemoveSession(id)
	if err != nil {
		tgSend(chatID, "❌ 删除失败: "+err.Error())
		return
	}
	// If we removed our own session, clear cached token
	sessMu.Lock()
	sessToken = ""
	sessMu.Unlock()
	tgSendKB(chatID, "✅ 会话已删除", [][]InlineButton{{{Text: "⬅️ 返回", CallbackData: "adm_sess"}}})
}

// ============================================================
// Utility functions for parameter parsing
// ============================================================

func parseKeyValueParams(s string) map[string]interface{} {
	params := make(map[string]interface{})
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			params[key] = val
		}
	}
	return params
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func parseInt(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}

func parseUint64(s string) uint64 {
	var u uint64
	fmt.Sscanf(s, "%d", &u)
	return u
}
