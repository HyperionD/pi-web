package piweb

import "encoding/json"

func assistantErrorMessage(raw json.RawMessage) string {
	var msg struct {
		Role         string `json:"role"`
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Role != "assistant" {
		return ""
	}
	if msg.StopReason != "error" || msg.ErrorMessage == "" {
		return ""
	}
	return msg.ErrorMessage
}
