package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// webhookHandler handles generic message forwarding to Telegram + WeCom
func webhookHandler(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Sendkey string `json:"sendkey"`
		Token   string `json:"token"`
		Text    string `json:"text"`
		Msg     string `json:"msg"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	// Auth check
	key := req.Sendkey
	if key == "" {
		key = req.Token
	}
	if key == "" {
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

	// Get message from any supported field
	msg := req.Text
	if msg == "" {
		msg = req.Msg
	}
	if msg == "" {
		msg = req.Content
	}
	if msg == "" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing text/msg/content"})
		return
	}

	var sent bool

	// Send to Telegram if configured
	if TelegramBotToken != "" && TelegramAllowed != "" {
		for _, idStr := range strings.Split(TelegramAllowed, ",") {
			var chatID int64
			fmt.Sscanf(strings.TrimSpace(idStr), "%d", &chatID)
			if chatID != 0 {
				tgSend(chatID, msg)
				sent = true
			}
		}
	}

	// Send to WeCom if configured
	if WecomCid != "" && WecomSecret != "" {
		token, err := getWecomAccessToken()
		if err == nil {
			wd := WecomMsg{ToUser: WecomToUid, MsgType: "text", AgentId: WecomAid}
			wd.Text.Content = msg
			httpDo("POST", fmt.Sprintf(SendMsgURL, token), wd, nil)
			sent = true
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"sent":   sent,
	})
}
