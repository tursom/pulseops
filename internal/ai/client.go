package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	httpClient  *http.Client
	endpoint    string
	apiKey      string
	model       string
	maxTokens   int
	temperature float64
}

type ClientConfig struct {
	Endpoint    string
	APIKey      string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	Temperature float64
}

func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		endpoint:    cfg.Endpoint,
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		maxTokens:   cfg.MaxTokens,
		temperature: cfg.Temperature,
	}
}

type ChatResponse struct {
	Content   string
	TokensIn  int
	TokensOut int
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletion struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) Chat(ctx context.Context, prompt string) (*ChatResponse, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}
	url := c.endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read chat response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("chat api error %d: %s", resp.StatusCode, string(respBody))
	}
	var completion chatCompletion
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return nil, fmt.Errorf("unmarshal chat response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("chat response has no choices")
	}
	return &ChatResponse{
		Content:   completion.Choices[0].Message.Content,
		TokensIn:  completion.Usage.PromptTokens,
		TokensOut: completion.Usage.CompletionTokens,
	}, nil
}
