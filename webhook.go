package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Komari webhook event structures
type KomariWebhookEvent struct {
	Event     string              `json:"event"`
	Type      string              `json:"type"`
	Name      string              `json:"name"`
	Nodes     []KomariWebhookNode `json:"nodes"`
	Clients   []KomariWebhookNode `json:"clients"`
	Time      string              `json:"time"`
	Timestamp string              `json:"timestamp"`
	Message   string              `json:"message"`
	Msg       string              `json:"msg"`
}

type KomariWebhookNode struct {
	Name    string `json:"name"`
	Client  string `json:"client"`
	UUID    string `json:"uuid"`
	IP      string `json:"ip"`
	IPv4    string `json:"ipv4"`
	Region  string `json:"region"`
	Load    string `json:"load"`
}

// Event emoji and name mappings
var webhookEventEmoji = map[string]string{
	"Offline": "🔴",
	"Online":  "🟢",
	"Alert":   "⚠️",
	"Renew":   "⏰",
	"Expire":  "🚨",
	"Test":    "🧪",
}

var webhookEventName = map[string]string{
	"Offline": "离线",
	"Online":  "上线",
	"Alert":   "告警",
	"Renew":   "续费",
	"Expire":  "到期",
	"Test":    "测试",
}

// formatWebhookEvent formats a Komari webhook event into a readable message
func formatWebhookEvent(event *KomariWebhookEvent) string {
	// Get event type from various possible fields
	eventType := event.Event
	if eventType == "" {
		eventType = event.Type
	}
	if eventType == "" {
		eventType = event.Name
	}
	if eventType == "" {
		eventType = "Unknown"
	}

	emoji := webhookEventEmoji[eventType]
	if emoji == "" {
		emoji = "📢"
	}
	name := webhookEventName[eventType]
	if name == "" {
		name = eventType
	}

	// Get nodes/clients list
	nodes := event.Nodes
	if len(nodes) == 0 {
		nodes = event.Clients
	}

	// Get time
	timeStr := event.Time
	if timeStr == "" {
		timeStr = event.Timestamp
	}

	// Get message
	message := event.Message
	if message == "" {
		message = event.Msg
	}

	var lines []string
	lines = append(lines, emoji+" Komari "+name+"通知")
	if timeStr != "" {
		lines = append(lines, "⏰ "+timeStr)
	}

	if len(nodes) > 0 {
		lines = append(lines, fmt.Sprintf("🖥️ 节点 (%d):", len(nodes)))
		for _, node := range nodes {
			nodeName := node.Name
			if nodeName == "" {
				nodeName = node.Client
			}
			if nodeName == "" {
				nodeName = node.UUID
			}
			if nodeName == "" {
				nodeName = "未知"
			}
			ip := node.IP
			if ip == "" {
				ip = node.IPv4
			}
			if ip == "" {
				ip = "-"
			}
			region := node.Region
			load := node.Load
			line := "  • " + nodeName + " (" + ip + ")"
			if region != "" {
				line += " " + region
			}
			if load != "" {
				line += " | 负载 " + load
			}
			lines = append(lines, line)
		}
	}

	if message != "" {
		lines = append(lines, "📝 "+message)
	}

	return strings.Join(lines, "\n")
}

// komariWebhookHandler handles Komari webhook events
func komariWebhookHandler(w http.ResponseWriter, r *http.Request) {
	// GET returns simple status for connectivity check
	if r.Method == "GET" {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "wecom-komari"})
		return
	}

	if r.Method != "POST" {
		w.WriteHeader(405)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	body, _ := io.ReadAll(r.Body)

	// Try to parse as webhook event
	var event KomariWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	// Check sendkey from various possible fields
	var req struct {
		Sendkey string `json:"sendkey"`
		Token   string `json:"token"`
	}
	json.Unmarshal(body, &req)
	key := req.Sendkey
	if key == "" {
		key = req.Token
	}
	if key == "" {
		// Try to get from query parameter
		key = r.URL.Query().Get("sendkey")
	}
	if key == "" {
		key = r.URL.Query().Get("token")
	}
	if key != Sendkey {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	// Format the event message
	text := formatWebhookEvent(&event)

	// Send to Telegram if configured
	if TelegramBotToken != "" && TelegramAllowed != "" {
		// Parse allowed users to get chat IDs
		for _, idStr := range strings.Split(TelegramAllowed, ",") {
			var chatID int64
			fmt.Sscanf(strings.TrimSpace(idStr), "%d", &chatID)
			if chatID != 0 {
				tgSend(chatID, text)
			}
		}
	}

	// Send to WeCom if configured
	if WecomCid != "" && WecomSecret != "" {
		token, err := getWecomAccessToken()
		if err == nil {
			wd := WecomMsg{ToUser: WecomToUid, MsgType: "text", AgentId: WecomAid}
			wd.Text.Content = text
			httpDo("POST", fmt.Sprintf(SendMsgURL, token), wd, nil)
		}
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
