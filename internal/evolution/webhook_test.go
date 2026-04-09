package evolution

import (
	"encoding/json"
	"testing"
)

func TestIsAudio(t *testing.T) {
	cases := []struct {
		msgType string
		want    bool
	}{
		{"audioMessage", true},
		{"voiceMessage", true},
		{"pttMessage", true},
		{"conversation", false},
		{"extendedTextMessage", false},
		{"imageMessage", false},
		{"", false},
	}
	for _, c := range cases {
		msg := &MessageData{MessageType: c.msgType}
		if got := msg.IsAudio(); got != c.want {
			t.Errorf("IsAudio(%q) = %v, want %v", c.msgType, got, c.want)
		}
	}
}

func TestTextContent_Conversation(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"conversation": "Olá tudo bem?"})
	msg := &MessageData{Message: raw}
	if got := msg.TextContent(); got != "Olá tudo bem?" {
		t.Errorf("TextContent() = %q, want 'Olá tudo bem?'", got)
	}
}

func TestTextContent_ExtendedText(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"text": "Extended hello"})
	msg := &MessageData{Message: raw}
	if got := msg.TextContent(); got != "Extended hello" {
		t.Errorf("TextContent() = %q, want 'Extended hello'", got)
	}
}

func TestTextContent_Empty(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{"audioMessage": map[string]string{}})
	msg := &MessageData{Message: raw}
	if got := msg.TextContent(); got != "" {
		t.Errorf("TextContent() = %q, want empty string for audio message", got)
	}
}

func TestTextContent_ConversationPreferredOverExtended(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{
		"conversation":        "Direct text",
		"extendedTextMessage": map[string]string{"text": "Extended text"},
	})
	msg := &MessageData{Message: raw}
	if got := msg.TextContent(); got != "Direct text" {
		t.Errorf("TextContent() = %q, want 'Direct text' (conversation preferred)", got)
	}
}
