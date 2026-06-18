package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config vars
var (
	Sendkey             = envDefault("SENDKEY", "set_a_sendkey")
	WecomCid            = envDefault("WECOM_CID", "")
	WecomSecret         = envDefault("WECOM_SECRET", "")
	WecomAid            = envDefault("WECOM_AID", "")
	WecomToUid          = envDefault("WECOM_TOUID", "@all")
	WecomToken          = envDefault("WECOM_TOKEN", "")
	WecomAESKey         = envDefault("WECOM_ENCODING_AES_KEY", "")
	KomariUrl           = envDefault("KOMARI_URL", "")
	KomariUser          = envDefault("KOMARI_USERNAME", "")
	KomariPass          = envDefault("KOMARI_PASSWORD", "")
	KomariApiKey        = envDefault("KOMARI_API_KEY", "")
	TelegramBotToken    = envDefault("TELEGRAM_BOT_TOKEN", "")
	TelegramWebhookSec  = envDefault("TELEGRAM_WEBHOOK_SECRET", "")
	TelegramAllowed     = envDefault("TELEGRAM_ALLOWED_USERS", "")
	TelegramAPIBase     = envDefault("TELEGRAM_API_BASE", "https://api.telegram.org")
	GetTokenURL         = "https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s"
	SendMsgURL          = "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s"
	MailSendURL         = "https://qyapi.weixin.qq.com/cgi-bin/exmail/app/compose_send?access_token=%s"
)

var (
	httpClient        = &http.Client{Timeout: 60 * time.Second}
	sessToken         string
	sessMu            sync.RWMutex
	wecomToken        string
	wecomTokenExpire  time.Time
	wecomTokenMu      sync.RWMutex
)

func envDefault(key, def string) string {
	if v, ok := getEnv(key); ok {
		return v
	}
	return def
}

func getEnv(key string) (string, bool) {
	v := os.Getenv(key)
	return v, v != ""
}

func httpDo(method, url string, body interface{}, headers map[string]string) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		j, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = strings.NewReader(string(j))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return b, nil
}

// Komari API
func getKomariToken() (string, error) {
	sessMu.RLock()
	if sessToken != "" {
		defer sessMu.RUnlock()
		return sessToken, nil
	}
	sessMu.RUnlock()
	if KomariUser == "" || KomariPass == "" {
		return "", fmt.Errorf("KOMARI_USERNAME/KOMARI_PASSWORD not set")
	}
	b, err := httpDo("POST", KomariUrl+"/api/login", map[string]string{"username": KomariUser, "password": KomariPass}, nil)
	if err != nil {
		return "", err
	}
	var r KomariLoginResp
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	if r.Status != "success" {
		return "", fmt.Errorf("login failed")
	}
	sessMu.Lock()
	sessToken = r.Data.SetCookie.SessionToken
	sessMu.Unlock()
	return sessToken, nil
}

func komariReq(method, path string) ([]byte, error) {
	headers := map[string]string{}
	if KomariApiKey != "" {
		headers["Authorization"] = "Bearer " + KomariApiKey
	} else {
		t, err := getKomariToken()
		if err != nil {
			return nil, err
		}
		headers["Cookie"] = "session_token=" + t
	}
	return httpDo(method, KomariUrl+path, nil, headers)
}

func getNodeList() ([]KomariNode, error) {
	b, err := komariReq("GET", "/api/nodes")
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("%s", r.Message)
	}
	var nodes []KomariNode
	if err := json.Unmarshal(r.Data, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func getNodeByUUID(uuid string) (*KomariNode, error) {
	nodes, err := getNodeList()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(uuid)
	for _, n := range nodes {
		if strings.ToLower(n.UUID) == q || strings.Contains(strings.ToLower(n.Name), q) {
			return &n, nil
		}
	}
	return nil, fmt.Errorf("node not found: %s", uuid)
}

func getNodeRealtime(uuid string) (*KomariRealtimeData, error) {
	b, err := komariReq("GET", "/api/recent/"+uuid)
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var data []KomariRealtimeData
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("no data")
	}
	return &data[len(data)-1], nil
}

func getNodeLoadHistory(uuid string, hours int) ([]KomariLoadRecord, error) {
	b, err := komariReq("GET", fmt.Sprintf("/api/records/load?uuid=%s&hours=%d", uuid, hours))
	if err != nil {
		return nil, err
	}
	var r struct {
		Status string `json:"status"`
		Data   struct {
			Count   int                `json:"count"`
			Records []KomariLoadRecord `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return r.Data.Records, nil
}

// New API functions

func getKomariPublicInfo() (*KomariPublicInfo, error) {
	b, err := komariReq("GET", "/api/public")
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var info KomariPublicInfo
	if err := json.Unmarshal(r.Data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func getKomariVersion() (*KomariVersion, error) {
	b, err := komariReq("GET", "/api/version")
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var v KomariVersion
	if err := json.Unmarshal(r.Data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func getKomariMe() (*KomariMe, error) {
	b, err := komariReq("GET", "/api/me")
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var m KomariMe
	if err := json.Unmarshal(r.Data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func getPingTasks() ([]KomariPingTask, error) {
	b, err := komariReq("GET", "/api/task/ping")
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var tasks []KomariPingTask
	if err := json.Unmarshal(r.Data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func getPingRecords() ([]KomariPingRecord, error) {
	b, err := komariReq("GET", "/api/records/ping")
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var records []KomariPingRecord
	if err := json.Unmarshal(r.Data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func getOnlineNodes() ([]KomariNode, error) {
	nodes, err := getNodeList()
	if err != nil {
		return nil, err
	}
	var online []KomariNode
	for _, n := range nodes {
		rt, err := getNodeRealtime(n.UUID)
		if err == nil && rt != nil {
			online = append(online, n)
		}
	}
	return online, nil
}

func getOfflineNodes() ([]KomariNode, error) {
	nodes, err := getNodeList()
	if err != nil {
		return nil, err
	}
	var offline []KomariNode
	for _, n := range nodes {
		_, err := getNodeRealtime(n.UUID)
		if err != nil {
			offline = append(offline, n)
		}
	}
	return offline, nil
}

func getNodesByGroup(groupName string) []KomariNode {
	nodes, err := getNodeList()
	if err != nil {
		return nil
	}
	var result []KomariNode
	g := strings.ToLower(groupName)
	for _, n := range nodes {
		if strings.ToLower(n.Group) == g {
			result = append(result, n)
		}
	}
	return result
}

func getNodeGroups() []string {
	nodes, err := getNodeList()
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var groups []string
	for _, n := range nodes {
		if n.Group != "" && !seen[n.Group] {
			seen[n.Group] = true
			groups = append(groups, n.Group)
		}
	}
	sort.Strings(groups)
	return groups
}

// Formatting

func fmtBytes(b uint64) string {
	const KB = 1024
	const MB = KB * 1024
	const GB = MB * 1024
	const TB = GB * 1024
	switch {
	case b >= TB:
		return fmt.Sprintf("%.2f TB", float64(b)/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func fmtDur(s uint64) string {
	d := s / 86400
	h := (s % 86400) / 3600
	m := (s % 3600) / 60
	if d > 0 {
		return fmt.Sprintf("%d天%d小时%d分", d, h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%d小时%d分", h, m)
	}
	return fmt.Sprintf("%d分钟", m)
}

func fmtSpeed(b float64) string {
	if b >= 1024*1024 {
		return fmt.Sprintf("%.2f MB/s", b/(1024*1024))
	}
	if b >= 1024 {
		return fmt.Sprintf("%.2f KB/s", b/1024)
	}
	return fmt.Sprintf("%.0f B/s", b)
}

func fmtNodeStatus(n *KomariNode, rt *KomariRealtimeData) string {
	var s strings.Builder
	s.WriteString(fmt.Sprintf("*%s*", n.Name))
	if n.Region != "" {
		s.WriteString(" " + n.Region)
	}
	s.WriteString("\n")
	if rt == nil {
		s.WriteString("🔴 离线\n")
		s.WriteString(fmt.Sprintf("📋 %s | %s\n", n.OS, n.Arch))
		s.WriteString(fmt.Sprintf("💾 内存: %s\n", fmtBytes(n.MemTotal)))
		s.WriteString(fmt.Sprintf("📁 磁盘: %s", fmtBytes(n.DiskTotal)))
		return s.String()
	}
	s.WriteString("🟢 在线\n")
	s.WriteString(fmt.Sprintf("📋 %s | %s\n", n.OS, n.Arch))
	if n.Virtualization != "" {
		s.WriteString(fmt.Sprintf("🔧 %s\n", n.Virtualization))
	}
	s.WriteString(fmt.Sprintf("🔲 CPU: %s (%d核) - %.1f%%\n", n.CPUName, n.CPUCores, rt.CPU.Usage))
	memPct := float64(rt.RAM.Used) / float64(n.MemTotal) * 100
	s.WriteString(fmt.Sprintf("💾 内存: %s / %s (%.1f%%)\n", fmtBytes(rt.RAM.Used), fmtBytes(n.MemTotal), memPct))
	if n.SwapTotal > 0 {
		sp := float64(rt.Swap.Used) / float64(n.SwapTotal) * 100
		s.WriteString(fmt.Sprintf("💿 Swap: %s / %s (%.1f%%)\n", fmtBytes(rt.Swap.Used), fmtBytes(n.SwapTotal), sp))
	}
	dPct := float64(rt.Disk.Used) / float64(n.DiskTotal) * 100
	s.WriteString(fmt.Sprintf("📁 磁盘: %s / %s (%.1f%%)\n", fmtBytes(rt.Disk.Used), fmtBytes(n.DiskTotal), dPct))
	s.WriteString(fmt.Sprintf("🌐 网络: ↑%s ↓%s\n", fmtSpeed(rt.Network.Up), fmtSpeed(rt.Network.Down)))
	s.WriteString(fmt.Sprintf("📊 总流量: ↑%s ↓%s\n", fmtBytes(rt.Network.TotalUp), fmtBytes(rt.Network.TotalDown)))
	s.WriteString(fmt.Sprintf("📈 负载: %.2f / %.2f / %.2f\n", rt.Load.Load1, rt.Load.Load5, rt.Load.Load15))
	s.WriteString(fmt.Sprintf("🔗 TCP %d | UDP %d\n", rt.Connections.TCP, rt.Connections.UDP))
	s.WriteString(fmt.Sprintf("⚙️ 进程: %d\n", rt.Process))
	s.WriteString(fmt.Sprintf("⏰ 运行: %s", fmtDur(rt.Uptime)))
	if n.Group != "" {
		s.WriteString(fmt.Sprintf("\n📂 %s", n.Group))
	}
	if n.Tags != "" {
		s.WriteString(fmt.Sprintf("\n🏷 %s", n.Tags))
	}
	if n.Price > 0 {
		s.WriteString(fmt.Sprintf("\n💰 %.2f%s/%d天", n.Price, n.Currency, n.BillingCycle))
	}
	if n.ExpiredAt != "" && !strings.HasPrefix(n.ExpiredAt, "0001") {
		s.WriteString(fmt.Sprintf("\n📅 到期: %s", n.ExpiredAt[:10]))
	}
	return s.String()
}

func fmtNodeList(nodes []KomariNode) string {
	if len(nodes) == 0 {
		return "暂无节点"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("共 %d 个节点:\n\n", len(nodes)))
	for i, n := range nodes {
		emoji := "⚪"
		if n.Hidden {
			emoji = "🙈"
		}
		s.WriteString(fmt.Sprintf("%d. %s %s", i+1, emoji, n.Name))
		if n.Region != "" {
			s.WriteString(" " + n.Region)
		}
		if n.Group != "" {
			s.WriteString(" [" + n.Group + "]")
		}
		s.WriteString("\n")
	}
	return s.String()
}

func fmtNodeListWithStatus(nodes []KomariNode) string {
	if len(nodes) == 0 {
		return "暂无节点"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("共 %d 个节点:\n\n", len(nodes)))
	for i, n := range nodes {
		rt, _ := getNodeRealtime(n.UUID)
		emoji := "🔴"
		if rt != nil {
			emoji = "🟢"
		}
		if n.Hidden {
			emoji = "🙈"
		}
		s.WriteString(fmt.Sprintf("%d. %s %s", i+1, emoji, n.Name))
		if n.Region != "" {
			s.WriteString(" " + n.Region)
		}
		if n.Group != "" {
			s.WriteString(" [" + n.Group + "]")
		}
		s.WriteString("\n")
	}
	return s.String()
}

func fmtCPUUsageRank(nodes []KomariNode) string {
	type nodeCPU struct {
		Name string
		CPU  float64
	}
	var items []nodeCPU
	for _, n := range nodes {
		rt, err := getNodeRealtime(n.UUID)
		if err == nil && rt != nil {
			items = append(items, nodeCPU{Name: n.Name, CPU: rt.CPU.Usage})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CPU > items[j].CPU })
	limit := 10
	if len(items) < limit {
		limit = len(items)
	}
	var s strings.Builder
	s.WriteString("🔲 *CPU 使用率排名 Top 10*\n\n")
	for i := 0; i < limit; i++ {
		s.WriteString(fmt.Sprintf("%d. %s - %.1f%%\n", i+1, items[i].Name, items[i].CPU))
	}
	if len(items) == 0 {
		s.WriteString("暂无在线节点")
	}
	return s.String()
}

func fmtMemUsageRank(nodes []KomariNode) string {
	type nodeMem struct {
		Name    string
		Used    uint64
		Total   uint64
		Percent float64
	}
	var items []nodeMem
	for _, n := range nodes {
		rt, err := getNodeRealtime(n.UUID)
		if err == nil && rt != nil && n.MemTotal > 0 {
			pct := float64(rt.RAM.Used) / float64(n.MemTotal) * 100
			items = append(items, nodeMem{Name: n.Name, Used: rt.RAM.Used, Total: n.MemTotal, Percent: pct})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Percent > items[j].Percent })
	limit := 10
	if len(items) < limit {
		limit = len(items)
	}
	var s strings.Builder
	s.WriteString("💾 *内存使用率排名 Top 10*\n\n")
	for i := 0; i < limit; i++ {
		s.WriteString(fmt.Sprintf("%d. %s - %s/%s (%.1f%%)\n", i+1, items[i].Name, fmtBytes(items[i].Used), fmtBytes(items[i].Total), items[i].Percent))
	}
	if len(items) == 0 {
		s.WriteString("暂无在线节点")
	}
	return s.String()
}

func fmtNetUsageRank(nodes []KomariNode) string {
	type nodeNet struct {
		Name     string
		TotalUp  uint64
		TotalDown uint64
		Total    uint64
	}
	var items []nodeNet
	for _, n := range nodes {
		rt, err := getNodeRealtime(n.UUID)
		if err == nil && rt != nil {
			total := rt.Network.TotalUp + rt.Network.TotalDown
			items = append(items, nodeNet{Name: n.Name, TotalUp: rt.Network.TotalUp, TotalDown: rt.Network.TotalDown, Total: total})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Total > items[j].Total })
	limit := 10
	if len(items) < limit {
		limit = len(items)
	}
	var s strings.Builder
	s.WriteString("🌐 *网络流量排名 Top 10*\n\n")
	for i := 0; i < limit; i++ {
		s.WriteString(fmt.Sprintf("%d. %s - ↑%s ↓%s\n", i+1, items[i].Name, fmtBytes(items[i].TotalUp), fmtBytes(items[i].TotalDown)))
	}
	if len(items) == 0 {
		s.WriteString("暂无在线节点")
	}
	return s.String()
}

func fmtSiteInfo() string {
	info, err := getKomariPublicInfo()
	if err != nil {
		return "❌ 获取站点信息失败: " + err.Error()
	}
	ver, _ := getKomariVersion()
	var s strings.Builder
	s.WriteString("ℹ️ *站点信息*\n\n")
	s.WriteString(fmt.Sprintf("📛 名称: %s\n", info.Sitename))
	if info.Description != "" {
		s.WriteString(fmt.Sprintf("📝 描述: %s\n", info.Description))
	}
	if info.Theme != "" {
		s.WriteString(fmt.Sprintf("🎨 主题: %s\n", info.Theme))
	}
	s.WriteString(fmt.Sprintf("📊 记录: %s\n", boolStr(info.RecordEnabled)))
	if info.RecordEnabled && info.RecordPreserveTime > 0 {
		s.WriteString(fmt.Sprintf("⏰ 保留时间: %d天\n", info.RecordPreserveTime/(3600*24)))
	}
	s.WriteString(fmt.Sprintf("🔒 私有站点: %s\n", boolStr(info.PrivateSite)))
	if ver != nil {
		s.WriteString(fmt.Sprintf("\n🚀 版本: %s", ver.Version))
		if ver.Hash != "" {
			s.WriteString(fmt.Sprintf(" (%s)", ver.Hash[:7]))
		}
	}
	return s.String()
}

func fmtPingInfo() string {
	tasks, err := getPingTasks()
	if err != nil {
		return "❌ 获取Ping任务失败: " + err.Error()
	}
	if len(tasks) == 0 {
		return "📋 暂无Ping任务"
	}
	var s strings.Builder
	s.WriteString("📡 *Ping 任务列表*\n\n")
	for _, t := range tasks {
		status := "✅"
		if !t.DefaultOn {
			status = "⏸"
		}
		s.WriteString(fmt.Sprintf("%s %s (ID: %d)\n", status, t.Name, t.ID))
		s.WriteString(fmt.Sprintf("   类型: %s | 间隔: %ds\n", t.Type, t.Interval))
		if len(t.Clients) > 0 {
			s.WriteString(fmt.Sprintf("   客户端: %d个\n", len(t.Clients)))
		}
	}
	records, err := getPingRecords()
	if err == nil && len(records) > 0 {
		s.WriteString(fmt.Sprintf("\n📊 最近记录: %d条\n", len(records)))
	}
	return s.String()
}

func fmtOfflineNodes() string {
	offline, err := getOfflineNodes()
	if err != nil {
		return "❌ 获取离线节点失败: " + err.Error()
	}
	if len(offline) == 0 {
		return "✅ 所有节点在线"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("🔴 *离线节点 (%d个)*\n\n", len(offline)))
	for i, n := range offline {
		s.WriteString(fmt.Sprintf("%d. %s", i+1, n.Name))
		if n.Region != "" {
			s.WriteString(" " + n.Region)
		}
		if n.Group != "" {
			s.WriteString(" [" + n.Group + "]")
		}
		s.WriteString("\n")
	}
	return s.String()
}

func fmtGroupList() string {
	groups := getNodeGroups()
	if len(groups) == 0 {
		return "暂无分组"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("📂 *节点分组 (%d个)*\n\n", len(groups)))
	for _, g := range groups {
		nodes := getNodesByGroup(g)
		s.WriteString(fmt.Sprintf("• %s (%d个节点)\n", g, len(nodes)))
	}
	return s.String()
}

func boolStr(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

func searchNodes(q string) []KomariNode {
	nodes, err := getNodeList()
	if err != nil {
		return nil
	}
	q = strings.ToLower(q)
	var r []KomariNode
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.Name), q) || strings.Contains(strings.ToLower(n.UUID), q) || strings.Contains(strings.ToLower(n.Group), q) {
			r = append(r, n)
		}
	}
	return r
}

func isUserAllowed(uid int64) bool {
	if TelegramAllowed == "" {
		return true
	}
	for _, id := range strings.Split(TelegramAllowed, ",") {
		if strings.TrimSpace(id) == fmt.Sprintf("%d", uid) {
			return true
		}
	}
	return false
}

// WeChat Work access token
func getWecomAccessToken() (string, error) {
	wecomTokenMu.RLock()
	if wecomToken != "" && time.Now().Before(wecomTokenExpire) {
		defer wecomTokenMu.RUnlock()
		return wecomToken, nil
	}
	wecomTokenMu.RUnlock()
	if WecomCid == "" || WecomSecret == "" {
		return "", fmt.Errorf("WECOM_CID/WECOM_SECRET not set")
	}
	b, err := httpDo("GET", fmt.Sprintf(GetTokenURL, WecomCid, WecomSecret), nil, nil)
	if err != nil {
		logger.Printf("[WecomToken] Get token failed: %v", err)
		return "", err
	}
	logger.Printf("[WecomToken] Token API response: %s", string(b))
	var r struct {
		Errcode     int    `json:"errcode"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	json.Unmarshal(b, &r)
	if r.Errcode != 0 {
		logger.Printf("[WecomToken] Token error: %d", r.Errcode)
		return "", fmt.Errorf("wecom token error: %d", r.Errcode)
	}
	wecomTokenMu.Lock()
	wecomToken = r.AccessToken
	wecomTokenExpire = time.Now().Add(time.Duration(r.ExpiresIn-300) * time.Second)
	wecomTokenMu.Unlock()
	return wecomToken, nil
}

// Admin API helper - POST with body
func komariReqBody(method, path string, body interface{}) ([]byte, error) {
	headers := map[string]string{}
	if KomariApiKey != "" {
		headers["Authorization"] = "Bearer " + KomariApiKey
	} else {
		t, err := getKomariToken()
		if err != nil {
			return nil, err
		}
		headers["Cookie"] = "session_token=" + t
	}
	return httpDo(method, KomariUrl+path, body, headers)
}

// Admin API: parse standard Komari response and return raw data
func komariAdminGetData(method, path string, body interface{}) (json.RawMessage, error) {
	var b []byte
	var err error
	if body != nil {
		b, err = komariReqBody(method, path, body)
	} else {
		b, err = komariReq(method, path)
	}
	if err != nil {
		return nil, err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("%s", r.Message)
	}
	return r.Data, nil
}

// Admin API: parse standard Komari response for action (no data)
func komariAdminAction(method, path string, body interface{}) error {
	var b []byte
	var err error
	if body != nil {
		b, err = komariReqBody(method, path, body)
	} else {
		b, err = komariReq(method, path)
	}
	if err != nil {
		return err
	}
	var r KomariAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	if r.Status != "success" {
		return fmt.Errorf("%s", r.Message)
	}
	return nil
}

// Client management
func adminListClients() ([]KomariNode, error) {
	data, err := komariAdminGetData("GET", "/api/admin/client/list", nil)
	if err != nil {
		return nil, err
	}
	var clients []KomariNode
	if err := json.Unmarshal(data, &clients); err != nil {
		return nil, err
	}
	return clients, nil
}

func adminGetClient(uuid string) (*KomariNode, error) {
	data, err := komariAdminGetData("GET", "/api/admin/client/"+uuid, nil)
	if err != nil {
		return nil, err
	}
	var client KomariNode
	if err := json.Unmarshal(data, &client); err != nil {
		return nil, err
	}
	return &client, nil
}

func adminAddClient(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/client/add", params)
}

func adminEditClient(uuid string, params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/client/"+uuid+"/edit", params)
}

func adminRemoveClient(uuid string) error {
	return komariAdminAction("POST", "/api/admin/client/"+uuid+"/remove", nil)
}

func adminGetClientToken(uuid string) (string, error) {
	data, err := komariAdminGetData("GET", "/api/admin/client/"+uuid+"/token", nil)
	if err != nil {
		return "", err
	}
	var tokenData struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &tokenData); err != nil {
		// Try parsing as plain string
		var s string
		if err2 := json.Unmarshal(data, &s); err2 == nil {
			return s, nil
		}
		return string(data), nil
	}
	return tokenData.Token, nil
}

// Notification management - offline
func adminListOfflineNotifications() (json.RawMessage, error) {
	return komariAdminGetData("GET", "/api/admin/notification/offline", nil)
}

func adminEditOfflineNotification(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/notification/offline/edit", params)
}

func adminEnableOfflineNotification() error {
	return komariAdminAction("POST", "/api/admin/notification/offline/enable", nil)
}

func adminDisableOfflineNotification() error {
	return komariAdminAction("POST", "/api/admin/notification/offline/disable", nil)
}

// Notification management - load alerts
func adminListLoadAlerts() (json.RawMessage, error) {
	return komariAdminGetData("GET", "/api/admin/notification/load/", nil)
}

func adminAddLoadAlert(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/notification/load/add", params)
}

func adminDeleteLoadAlert(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/notification/load/delete", params)
}

func adminEditLoadAlert(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/notification/load/edit", params)
}

// Notification management - traffic reports
func adminListTrafficReports() (json.RawMessage, error) {
	return komariAdminGetData("GET", "/api/admin/notification/traffic-report/", nil)
}

func adminEditTrafficReport(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/notification/traffic-report/edit", params)
}

func adminEnableTrafficReport() error {
	return komariAdminAction("POST", "/api/admin/notification/traffic-report/enable", nil)
}

func adminDisableTrafficReport() error {
	return komariAdminAction("POST", "/api/admin/notification/traffic-report/disable", nil)
}

// Ping task management (admin)
func adminListPingTasks() ([]KomariPingTask, error) {
	data, err := komariAdminGetData("GET", "/api/admin/ping/", nil)
	if err != nil {
		return nil, err
	}
	var tasks []KomariPingTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func adminAddPingTask(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/ping/add", params)
}

func adminDeletePingTask(id int) error {
	return komariAdminAction("POST", "/api/admin/ping/delete", map[string]interface{}{"id": id})
}

func adminEditPingTask(id int, params map[string]interface{}) error {
	params["id"] = id
	return komariAdminAction("POST", "/api/admin/ping/edit", params)
}

// Settings management
func adminGetSettings() (map[string]interface{}, error) {
	data, err := komariAdminGetData("GET", "/api/admin/settings/", nil)
	if err != nil {
		return nil, err
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func adminEditSettings(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/settings/", params)
}

// Remote task management
func adminListAllTasks() (json.RawMessage, error) {
	return komariAdminGetData("GET", "/api/admin/task/all", nil)
}

func adminGetTask(taskID string) (json.RawMessage, error) {
	return komariAdminGetData("GET", "/api/admin/task/"+taskID, nil)
}

func adminGetTaskResult(taskID string) (json.RawMessage, error) {
	return komariAdminGetData("GET", "/api/admin/task/"+taskID+"/result", nil)
}

func adminExecCommand(command string) (json.RawMessage, error) {
	return komariAdminGetData("POST", "/api/admin/task/exec", map[string]interface{}{"command": command})
}

// Record management
func adminClearRecords(params map[string]interface{}) error {
	return komariAdminAction("POST", "/api/admin/record/clear", params)
}

func adminClearAllRecords() error {
	return komariAdminAction("POST", "/api/admin/record/clear/all", nil)
}

// Audit logs
func adminGetLogs(limit, page int) (json.RawMessage, error) {
	path := fmt.Sprintf("/api/admin/logs?limit=%d&page=%d", limit, page)
	return komariAdminGetData("GET", path, nil)
}

// Session management
func adminGetSessions() (json.RawMessage, error) {
	return komariAdminGetData("GET", "/api/admin/session/get", nil)
}

func adminRemoveSession(id string) error {
	return komariAdminAction("POST", "/api/admin/session/remove", map[string]interface{}{"id": id})
}

func adminRemoveAllSessions() error {
	return komariAdminAction("POST", "/api/admin/session/remove/all", nil)
}

// Admin formatting helpers

func fmtAdminClientList(clients []KomariNode) string {
	if len(clients) == 0 {
		return "📋 暂无客户端"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("📋 *管理客户端列表* (%d个)\n\n", len(clients)))
	for i, c := range clients {
		emoji := "⚪"
		if c.Hidden {
			emoji = "🙈"
		}
		s.WriteString(fmt.Sprintf("%d. %s %s", i+1, emoji, c.Name))
		if c.Region != "" {
			s.WriteString(" " + c.Region)
		}
		if c.Group != "" {
			s.WriteString(" [" + c.Group + "]")
		}
		s.WriteString("\n")
	}
	return s.String()
}

func fmtAdminClientDetail(c *KomariNode) string {
	var s strings.Builder
	s.WriteString(fmt.Sprintf("📋 *客户端详情*\n\n"))
	s.WriteString(fmt.Sprintf("📛 名称: %s\n", c.Name))
	s.WriteString(fmt.Sprintf("🔑 UUID: `%s`\n", c.UUID))
	if c.Region != "" {
		s.WriteString(fmt.Sprintf("🌍 区域: %s\n", c.Region))
	}
	if c.Group != "" {
		s.WriteString(fmt.Sprintf("📂 分组: %s\n", c.Group))
	}
	if c.Tags != "" {
		s.WriteString(fmt.Sprintf("🏷 标签: %s\n", c.Tags))
	}
	s.WriteString(fmt.Sprintf("📋 系统: %s %s\n", c.OS, c.Arch))
	s.WriteString(fmt.Sprintf("🔲 CPU: %s (%d核)\n", c.CPUName, c.CPUCores))
	s.WriteString(fmt.Sprintf("💾 内存: %s\n", fmtBytes(c.MemTotal)))
	s.WriteString(fmt.Sprintf("📁 磁盘: %s\n", fmtBytes(c.DiskTotal)))
	if c.Virtualization != "" {
		s.WriteString(fmt.Sprintf("🔧 虚拟化: %s\n", c.Virtualization))
	}
	s.WriteString(fmt.Sprintf("⚖️ 权重: %d\n", c.Weight))
	s.WriteString(fmt.Sprintf("🙈 隐藏: %s\n", boolStr(c.Hidden)))
	if c.Price > 0 {
		s.WriteString(fmt.Sprintf("💰 价格: %.2f%s/%d天\n", c.Price, c.Currency, c.BillingCycle))
	}
	if c.ExpiredAt != "" && !strings.HasPrefix(c.ExpiredAt, "0001") {
		s.WriteString(fmt.Sprintf("📅 到期: %s\n", c.ExpiredAt[:10]))
	}
	if c.PublicRemark != "" {
		s.WriteString(fmt.Sprintf("📝 备注: %s\n", c.PublicRemark))
	}
	return s.String()
}

func fmtAdminSettings(settings map[string]interface{}) string {
	var s strings.Builder
	s.WriteString("⚙️ *Komari 设置*\n\n")
	getStr := func(key string) string {
		if v, ok := settings[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return "-"
	}
	getBool := func(key string) string {
		if v, ok := settings[key]; ok {
			if b, ok := v.(bool); ok {
				return boolStr(b)
			}
		}
		return "-"
	}
	s.WriteString(fmt.Sprintf("📛 站点名: %s\n", getStr("sitename")))
	s.WriteString(fmt.Sprintf("📝 描述: %s\n", getStr("description")))
	s.WriteString(fmt.Sprintf("🎨 主题: %s\n", getStr("theme")))
	s.WriteString(fmt.Sprintf("📊 记录: %s\n", getBool("record_enabled")))
	s.WriteString(fmt.Sprintf("🔒 私有: %s\n", getBool("private_site")))
	s.WriteString(fmt.Sprintf("👁 访客看节点: %s\n", getBool("allow_guest_view_node")))
	s.WriteString(fmt.Sprintf("📈 访客看统计: %s\n", getBool("allow_guest_view_stats")))
	return s.String()
}

func fmtAdminLogs(data json.RawMessage) string {
	var logs []AdminAuditLog
	if err := json.Unmarshal(data, &logs); err != nil {
		// Try generic array
		var raw []map[string]interface{}
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return "📝 无法解析日志数据"
		}
		var s strings.Builder
		s.WriteString(fmt.Sprintf("📝 *审计日志* (%d条)\n\n", len(raw)))
		for i, log := range raw {
			if i >= 10 {
				s.WriteString(fmt.Sprintf("... 还有 %d 条\n", len(raw)-10))
				break
			}
			s.WriteString(fmt.Sprintf("%d. %v\n", i+1, log))
		}
		return s.String()
	}
	if len(logs) == 0 {
		return "📝 暂无审计日志"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("📝 *审计日志* (%d条)\n\n", len(logs)))
	limit := 10
	if len(logs) < limit {
		limit = len(logs)
	}
	for i := 0; i < limit; i++ {
		l := logs[i]
		s.WriteString(fmt.Sprintf("%d. `%s`", i+1, l.Action))
		if l.Username != "" {
			s.WriteString(fmt.Sprintf(" - %s", l.Username))
		}
		if l.IP != "" {
			s.WriteString(fmt.Sprintf(" (%s)", l.IP))
		}
		if l.CreatedAt != "" {
			ts := l.CreatedAt
			if len(ts) > 19 {
				ts = ts[:19]
			}
			s.WriteString(fmt.Sprintf(" %s", ts))
		}
		s.WriteString("\n")
	}
	if len(logs) > limit {
		s.WriteString(fmt.Sprintf("\n... 还有 %d 条", len(logs)-limit))
	}
	return s.String()
}

func fmtAdminSessions(data json.RawMessage) string {
	var sessions []AdminSession
	if err := json.Unmarshal(data, &sessions); err != nil {
		var raw []map[string]interface{}
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return "🔑 无法解析会话数据"
		}
		var s strings.Builder
		s.WriteString(fmt.Sprintf("🔑 *活跃会话* (%d个)\n\n", len(raw)))
		for i, sess := range raw {
			if i >= 10 {
				break
			}
			s.WriteString(fmt.Sprintf("%d. %v\n", i+1, sess))
		}
		return s.String()
	}
	if len(sessions) == 0 {
		return "🔑 暂无活跃会话"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("🔑 *活跃会话* (%d个)\n\n", len(sessions)))
	limit := 10
	if len(sessions) < limit {
		limit = len(sessions)
	}
	for i := 0; i < limit; i++ {
		sess := sessions[i]
		name := sess.Username
		if name == "" {
			name = fmt.Sprintf("User#%d", sess.UserID)
		}
		s.WriteString(fmt.Sprintf("%d. %s", i+1, name))
		if sess.IP != "" {
			s.WriteString(fmt.Sprintf(" (%s)", sess.IP))
		}
		if sess.CreatedAt != "" {
			ts := sess.CreatedAt
			if len(ts) > 19 {
				ts = ts[:19]
			}
			s.WriteString(fmt.Sprintf(" %s", ts))
		}
		s.WriteString("\n")
	}
	return s.String()
}

func fmtAdminTasks(data json.RawMessage) string {
	var tasks []AdminRemoteTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		var raw []map[string]interface{}
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return "⚡ 无法解析任务数据"
		}
		var s strings.Builder
		s.WriteString(fmt.Sprintf("⚡ *远程任务* (%d个)\n\n", len(raw)))
		for i, t := range raw {
			if i >= 10 {
				break
			}
			s.WriteString(fmt.Sprintf("%d. %v\n", i+1, t))
		}
		return s.String()
	}
	if len(tasks) == 0 {
		return "⚡ 暂无远程任务"
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("⚡ *远程任务* (%d个)\n\n", len(tasks)))
	limit := 10
	if len(tasks) < limit {
		limit = len(tasks)
	}
	for i := 0; i < limit; i++ {
		t := tasks[i]
		s.WriteString(fmt.Sprintf("%d. `%s`", i+1, t.Command))
		if t.Status != "" {
			s.WriteString(fmt.Sprintf(" [%s]", t.Status))
		}
		if t.CreatedAt != "" {
			ts := t.CreatedAt
			if len(ts) > 19 {
				ts = ts[:19]
			}
			s.WriteString(fmt.Sprintf(" %s", ts))
		}
		s.WriteString("\n")
	}
	return s.String()
}

func fmtAdminNotifications(data json.RawMessage, title string) string {
	var configs []AdminNotificationConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		var raw []map[string]interface{}
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return fmt.Sprintf("🔔 无法解析%s数据", title)
		}
		if len(raw) == 0 {
			return fmt.Sprintf("🔔 暂无%s配置", title)
		}
		var s strings.Builder
		s.WriteString(fmt.Sprintf("🔔 *%s* (%d个)\n\n", title, len(raw)))
		for i, c := range raw {
			s.WriteString(fmt.Sprintf("%d. %v\n", i+1, c))
		}
		return s.String()
	}
	if len(configs) == 0 {
		return fmt.Sprintf("🔔 暂无%s配置", title)
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf("🔔 *%s* (%d个)\n\n", title, len(configs)))
	for i, c := range configs {
		status := "✅"
		if !c.Enabled {
			status = "⏸"
		}
		s.WriteString(fmt.Sprintf("%d. %s %s", i+1, status, c.Name))
		if c.Type != "" {
			s.WriteString(fmt.Sprintf(" (%s)", c.Type))
		}
		s.WriteString("\n")
	}
	return s.String()
}
