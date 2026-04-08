package whisper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

type Client struct {
	baseURL  string
	model    string
	language string
	http     *http.Client
}

type TranslationResponse struct {
	Text string `json:"text"`
}

func NewClient(baseURL, model, language string) *Client {
	return &Client{
		baseURL:  baseURL,
		model:    model,
		language: language,
		http: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// Transcribe returns the raw transcript in the source language (no translation).
func (c *Client) Transcribe(filename string, audio io.Reader) (string, error) {
	return c.callSpeaches("/v1/audio/transcriptions", filename, audio)
}

// Translate transcribes and translates to English in one step (Whisper built-in).
// Used as fallback when ollama is unavailable.
func (c *Client) Translate(filename string, audio io.Reader) (string, error) {
	return c.callSpeaches("/v1/audio/translations", filename, audio)
}

func (c *Client) callSpeaches(endpoint, filename string, audio io.Reader) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, audio); err != nil {
		return "", fmt.Errorf("copying audio data: %w", err)
	}

	w.WriteField("model", c.model)
	w.WriteField("language", c.language)
	w.WriteField("response_format", "json")

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("speaches returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result TranslationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return result.Text, nil
}

func (c *Client) Healthy() bool {
	resp, err := c.http.Get(c.baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
