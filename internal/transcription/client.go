package transcription

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TranscriptionResponse struct {
	Text string `json:"text"`
}

type Config struct{ Endpoint, APIKey, Model string }
type Client struct {
	config Config
	http   *http.Client
}

func NewClient(config Config) (*Client, error) {
	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.Model = strings.TrimSpace(config.Model)
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, fmt.Errorf("TRANSCRIPTION_ENDPOINT must be an absolute HTTP(S) URL without credentials or a fragment")
	}
	if config.APIKey == "" {
		return nil, fmt.Errorf("TRANSCRIPTION_API_KEY is required")
	}
	if config.Model == "" {
		return nil, fmt.Errorf("TRANSCRIPTION_MODEL is required")
	}
	return &Client{config: config, http: &http.Client{Timeout: 20 * time.Second}}, nil
}

// TranscribeAudio sends an OpenAI-compatible multipart transcription request.
func (c *Client) TranscribeAudio(ctx context.Context, audioData []byte) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.ogg")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err = io.Copy(part, bytes.NewReader(audioData)); err != nil {
		return "", fmt.Errorf("failed to copy audio content: %w", err)
	}

	if err := writer.WriteField("model", c.config.Model); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.config.Endpoint, body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcription API returned HTTP %d", resp.StatusCode)
	}

	var result TranscriptionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Text, nil
}
