package evolution

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL  string
	apiKey   string
	instance string
	http     *http.Client
}

func NewClient(baseURL, apiKey, instance string) *Client {
	return &Client{
		baseURL:  baseURL,
		apiKey:   apiKey,
		instance: instance,
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

type mediaRequest struct {
	Message      map[string]json.RawMessage `json:"message"`
	ConvertToMp4 bool                       `json:"convertToMp4"`
}

type mediaResponse struct {
	Base64   string `json:"base64"`
	Mimetype string `json:"mimetype"`
}

func (c *Client) DownloadMediaByMessage(messageJSON json.RawMessage) ([]byte, string, error) {
	var rawMsg map[string]json.RawMessage
	if err := json.Unmarshal(messageJSON, &rawMsg); err != nil {
		return nil, "", fmt.Errorf("parsing message data: %w", err)
	}

	payload := mediaRequest{Message: rawMsg}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling media request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/getBase64FromMediaMessage/%s", c.baseURL, c.instance)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("creating media request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("downloading media: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading media response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("media download returned %d: %s", resp.StatusCode, string(respBody))
	}

	var media mediaResponse
	if err := json.Unmarshal(respBody, &media); err != nil {
		return nil, "", fmt.Errorf("decoding media response: %w", err)
	}

	data, err := base64.StdEncoding.DecodeString(media.Base64)
	if err != nil {
		return nil, "", fmt.Errorf("decoding base64 media: %w", err)
	}

	return data, media.Mimetype, nil
}

type sendTextPayload struct {
	Number string `json:"number"`
	Text   string `json:"text"`
	Delay  int    `json:"delay"`
}

type Group struct {
	ID           string        `json:"id"`
	Subject      string        `json:"subject"`
	Participants []Participant `json:"participants"`
	Size         int           `json:"size"`
}

type Participant struct {
	ID    string `json:"id"`
	Admin string `json:"admin"`
}

func (c *Client) FetchGroups(withParticipants bool) ([]Group, error) {
	params := "false"
	if withParticipants {
		params = "true"
	}
	url := fmt.Sprintf("%s/group/fetchAllGroups/%s?getParticipants=%s", c.baseURL, c.instance, params)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("apikey", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching groups: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("groups API returned %d: %s", resp.StatusCode, string(body))
	}

	var groups []Group
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("decoding groups: %w", err)
	}

	for i := range groups {
		groups[i].Size = len(groups[i].Participants)
	}
	return groups, nil
}

type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) FetchLabels() ([]Label, error) {
	url := fmt.Sprintf("%s/label/findLabels/%s", c.baseURL, c.instance)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("apikey", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching labels: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading labels response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("labels API returned %d: %s", resp.StatusCode, string(body))
	}

	var labels []Label
	if err := json.Unmarshal(body, &labels); err != nil {
		return nil, fmt.Errorf("decoding labels: %w", err)
	}
	return labels, nil
}

type handleLabelPayload struct {
	Number  string `json:"number"`
	LabelID string `json:"labelId"`
	Action  string `json:"action"`
}

func (c *Client) HandleLabel(jid, labelID, action string) error {
	number := jid
	if idx := len(jid) - len("@s.whatsapp.net"); idx > 0 && jid[idx:] == "@s.whatsapp.net" {
		number = jid[:idx]
	}

	payload := handleLabelPayload{Number: number, LabelID: labelID, Action: action}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling label payload: %w", err)
	}

	url := fmt.Sprintf("%s/label/handleLabel/%s", c.baseURL, c.instance)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("handling label: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("handleLabel returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) SendText(to, text string) error {
	payload := sendTextPayload{
		Number: to,
		Text:   text,
		Delay:  0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	url := fmt.Sprintf("%s/message/sendText/%s", c.baseURL, c.instance)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("evolution API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
