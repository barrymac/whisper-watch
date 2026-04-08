package evolution

import "encoding/json"

type LabelAssociationData struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color int    `json:"color"`
	Type  string `json:"type"`
}

type LabelAssociationEvent struct {
	Contact string               `json:"contact"`
	Label   LabelAssociationData `json:"label"`
}

type WebhookPayload struct {
	Event    string          `json:"event"`
	Instance string          `json:"instance"`
	Data     json.RawMessage `json:"data"`
}

type MessageData struct {
	Key struct {
		RemoteJid string `json:"remoteJid"`
		FromMe    bool   `json:"fromMe"`
		ID        string `json:"id"`
	} `json:"key"`
	PushName    string          `json:"pushName"`
	MessageType string          `json:"messageType"`
	Message     json.RawMessage `json:"message"`
	MediaURL    string          `json:"mediaUrl"`
	Raw         json.RawMessage `json:"-"`
}

type AudioMessage struct {
	URL      string `json:"url"`
	Mimetype string `json:"mimetype"`
}

type TextMessage struct {
	Conversation string `json:"conversation"`
}

type ExtendedTextMessage struct {
	Text string `json:"text"`
}

func ParseMessageData(raw json.RawMessage) (*MessageData, error) {
	var msg MessageData
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	msg.Raw = raw
	return &msg, nil
}

func (m *MessageData) IsAudio() bool {
	return m.MessageType == "audioMessage" || m.MessageType == "voiceMessage" || m.MessageType == "pttMessage"
}

func (m *MessageData) TextContent() string {
	var text TextMessage
	if err := json.Unmarshal(m.Message, &text); err == nil && text.Conversation != "" {
		return text.Conversation
	}
	var ext ExtendedTextMessage
	if err := json.Unmarshal(m.Message, &ext); err == nil && ext.Text != "" {
		return ext.Text
	}
	return ""
}
