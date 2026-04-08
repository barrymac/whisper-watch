package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const alreadyEnglish = "ALREADY_ENGLISH"

type Client struct {
	baseURL string
	mu      sync.RWMutex
	model   string
	http    *http.Client
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
}

const transcriptPrompt = `You are a professional translator specialising in Brazilian Portuguese to English.

The input is a speech-to-text transcript from a non-native Portuguese speaker with an Irish accent speaking Brazilian Portuguese (Bahian dialect, Salvador/Bahia region). The transcript may contain:
- Mishearing caused by the foreign accent — phonetically similar but wrong Portuguese words
- Irish-inflected pronunciation of Portuguese vowels and consonants
- Bahian regional expressions and vocabulary

Your task:
1. Silently correct any transcription errors using context and likely intended meaning
2. Translate the corrected text to natural, fluent English
3. Return ONLY the English translation — no explanations, no notes, no original text`

const textTranslatePrompt = `You are a professional translator specialising in Brazilian Portuguese to English.

If the message is already written in English, reply with exactly: ALREADY_ENGLISH

Otherwise translate it to natural, fluent English.
Return ONLY the translation — no explanations, no notes, no original text.`

const replyPrompt = `You are a helpful assistant drafting a short, professional reply in Brazilian Portuguese (Bahian style, warm and friendly).

The message below has just been received and translated. Draft a natural, concise reply the recipient could send back.
Return ONLY the draft reply in Portuguese — no explanations, no English, no preamble.`

const toPortuguesePrompt = `You are a professional translator specialising in English to Brazilian Portuguese (Bahian dialect, Salvador/Bahia region).

Translate the English text to natural, warm Brazilian Portuguese with Bahian regional style.
Return ONLY the Portuguese translation — no explanations, no notes, no original text.`

const summaryPrompt = `Summarise the following WhatsApp messages received today. Group by contact, note key topics, any urgent requests, and overall tone.
Keep it concise — bullet points preferred. No filler.`

func (c *Client) FixAndTranslate(portugueseText string) (string, error) {
	return c.chat(transcriptPrompt, portugueseText)
}

func (c *Client) TranslateText(text string) (string, bool, error) {
	result, err := c.chat(textTranslatePrompt, text)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(result) == alreadyEnglish {
		return "", true, nil
	}
	return result, false, nil
}

func (c *Client) DraftReply(translatedText string) (string, error) {
	return c.chat(replyPrompt, translatedText)
}

func (c *Client) TranslateToPortuguese(englishText string) (string, error) {
	return c.chat(toPortuguesePrompt, englishText)
}

func (c *Client) Summarise(messagesBlock string) (string, error) {
	return c.chat(summaryPrompt, messagesBlock)
}

func (c *Client) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

func (c *Client) SetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = model
}

type ModelInfo struct {
	Name string
	Size int64
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"models"`
}

func (c *Client) ListModels() ([]ModelInfo, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(body))
	}

	var tags tagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("decoding models: %w", err)
	}

	models := make([]ModelInfo, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = ModelInfo{Name: m.Name, Size: m.Size}
	}
	return models, nil
}

func (c *Client) chat(system, user string) (string, error) {
	c.mu.RLock()
	model := c.model
	c.mu.RUnlock()
	payload := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling ollama: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result chatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return result.Message.Content, nil
}
