package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
		return "", err
	}
	var r struct {
		Errcode     int    `json:"errcode"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	json.Unmarshal(b, &r)
	if r.Errcode != 0 {
		return "", fmt.Errorf("wecom token error: %d", r.Errcode)
	}
	wecomTokenMu.Lock()
	wecomToken = r.AccessToken
	wecomTokenExpire = time.Now().Add(time.Duration(r.ExpiresIn-300) * time.Second)
	wecomTokenMu.Unlock()
	return wecomToken, nil
}
