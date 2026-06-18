package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type KomariAPIResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type KomariLoginResp struct {
	Status string `json:"status"`
	Data   struct {
		SetCookie struct {
			SessionToken string `json:"session_token"`
		} `json:"set-cookie"`
	} `json:"data"`
}

type KomariNode struct {
	UUID             string  `json:"uuid"`
	Name             string  `json:"name"`
	CPUName          string  `json:"cpu_name"`
	Virtualization   string  `json:"virtualization"`
	Arch             string  `json:"arch"`
	CPUCores         int     `json:"cpu_cores"`
	OS               string  `json:"os"`
	KernelVersion    string  `json:"kernel_version"`
	GPUName          string  `json:"gpu_name"`
	Region           string  `json:"region"`
	MemTotal         uint64  `json:"mem_total"`
	SwapTotal        uint64  `json:"swap_total"`
	DiskTotal        uint64  `json:"disk_total"`
	Weight           int     `json:"weight"`
	Price            float64 `json:"price"`
	BillingCycle     int     `json:"billing_cycle"`
	AutoRenewal      bool    `json:"auto_renewal"`
	Currency         string  `json:"currency"`
	ExpiredAt        string  `json:"expired_at"`
	Group            string  `json:"group"`
	Tags             string  `json:"tags"`
	PublicRemark     string  `json:"public_remark"`
	Hidden           bool    `json:"hidden"`
	TrafficLimit     uint64  `json:"traffic_limit"`
	TrafficLimitType string  `json:"traffic_limit_type"`
	Token            string  `json:"token,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type KomariRealtimeData struct {
	CPU struct {
		Usage float64 `json:"usage"`
	} `json:"cpu"`
	RAM struct {
		Total uint64 `json:"total"`
		Used  uint64 `json:"used"`
	} `json:"ram"`
	Swap struct {
		Total uint64 `json:"total"`
		Used  uint64 `json:"used"`
	} `json:"swap"`
	Load struct {
		Load1  float64 `json:"load1"`
		Load5  float64 `json:"load5"`
		Load15 float64 `json:"load15"`
	} `json:"load"`
	Disk struct {
		Total uint64 `json:"total"`
		Used  uint64 `json:"used"`
	} `json:"disk"`
	Network struct {
		Up        float64 `json:"up"`
		Down      float64 `json:"down"`
		TotalUp   uint64  `json:"totalUp"`
		TotalDown uint64  `json:"totalDown"`
	} `json:"network"`
	Connections struct {
		TCP int `json:"tcp"`
		UDP int `json:"udp"`
	} `json:"connections"`
	Uptime    uint64 `json:"uptime"`
	Process   int    `json:"process"`
	UpdatedAt string `json:"updated_at"`
}

type KomariLoadRecord struct {
	Client  string  `json:"client"`
	Time    string  `json:"time"`
	CPU     float64 `json:"cpu"`
	RAM     uint64  `json:"ram"`
	Disk    uint64  `json:"disk"`
	Load    float64 `json:"load"`
	NetIn   float64 `json:"net_in"`
	NetOut  float64 `json:"net_out"`
	Process int     `json:"process"`
}

type KomariPingTask struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Clients   []string `json:"clients"`
	DefaultOn bool     `json:"default_on"`
	Type      string   `json:"type"`
	Interval  int      `json:"interval"`
}

type KomariPingRecord struct {
	TaskID int     `json:"task_id"`
	Time   string  `json:"time"`
	Value  float64 `json:"value"`
	Client string  `json:"client"`
}

type KomariPublicInfo struct {
	Sitename            string `json:"sitename"`
	Description         string `json:"description"`
	Theme               string `json:"theme"`
	RecordEnabled       bool   `json:"record_enabled"`
	RecordPreserveTime  int    `json:"record_preserve_time"`
	PrivateSite         bool   `json:"private_site"`
	AllowGuestViewNode  bool   `json:"allow_guest_view_node"`
	AllowGuestViewStats bool   `json:"allow_guest_view_stats"`
}

type KomariVersion struct {
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

type KomariMe struct {
	LoggedIn   bool   `json:"logged_in"`
	Username   string `json:"username"`
	UUID       string `json:"uuid"`
	TwoFA      bool   `json:"2fa_enabled"`
	SSOID      string `json:"sso_id"`
	SSOType    string `json:"sso_type"`
}

type InlineKeyboard struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type TgMessage struct {
	ChatID    int64            `json:"chat_id"`
	Text      string           `json:"text"`
	ParseMode string           `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboard `json:"reply_markup,omitempty"`
}

type TgUpdate struct {
	UpdateID      int64           `json:"update_id"`
	Message       *TgMsg          `json:"message,omitempty"`
	CallbackQuery *TgCallback     `json:"callback_query,omitempty"`
}

type TgMsg struct {
	MessageID int64  `json:"message_id"`
	From      TgUser `json:"from"`
	Chat      TgChat `json:"chat"`
	Text      string `json:"text,omitempty"`
}

type TgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

type TgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type TgCallback struct {
	ID      string  `json:"id"`
	From    TgUser  `json:"from"`
	Message *TgMsg  `json:"message,omitempty"`
	Data    string  `json:"data,omitempty"`
}

type WecomMsg struct {
	ToUser  string `json:"touser"`
	AgentId string `json:"agentid"`
	MsgType string `json:"msgtype"`
	Text    struct{ Content string `json:"content"` } `json:"text"`
	MD      struct{ Content string `json:"content"` } `json:"markdown"`
}

type FlexibleInt64 int64

func (f *FlexibleInt64) UnmarshalJSON(data []byte) error {
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexibleInt64(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("FlexibleInt64: cannot parse %s", string(data))
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = FlexibleInt64(n)
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		*f = FlexibleInt64(t.Unix())
		return nil
	}
	return fmt.Errorf("FlexibleInt64: cannot parse %q", s)
}

// Admin API types

type AdminNotificationConfig struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type"`
	Interval int    `json:"interval"`
}

type AdminRemoteTask struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Command   string `json:"command"`
	Status    string `json:"status"`
	Result    string `json:"result"`
	CreatedAt string `json:"created_at"`
}

type AdminAuditLog struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	Action    string `json:"action"`
	Detail    string `json:"detail"`
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
}

type AdminSession struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}
