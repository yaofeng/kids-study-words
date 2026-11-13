// Package ai talks to an OpenAI-compatible chat completion endpoint to write
// extra example sentences for a word.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Example is one generated sentence pair.
type Example struct {
	EN string `json:"en"`
	ZH string `json:"zh"`
}

const (
	maxExamples        = 3
	maxSentenceRunes   = 300
	systemPrompt       = "你是儿童英语词典编辑，面向 8–12 岁的中国孩子。只用简单、常见的英文单词，句子要短。只输出 JSON，不要解释。"
	maxResponseBytes   = 1 << 20
	emptyContentErrMsg = "ai: response has no content"
)

// Client is an OpenAI-compatible chat completion client.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// New builds a client. A zero timeout means fifteen seconds.
func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		http:    &http.Client{Timeout: timeout},
	}
}

// Enabled reports whether the client has everything it needs to make calls.
func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != "" && c.apiKey != "" && c.model != ""
}

// Model returns the configured model name.
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// Host returns the base URL's host for display, or an empty string.
func (c *Client) Host() string {
	if c == nil {
		return ""
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return ""
	}
	return u.Host
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Examples asks the model for up to three sentence pairs for word. Every
// returned sentence must contain the word or one of its forms.
func (c *Client) Examples(ctx context.Context, word, translation string, forms []string) ([]Example, error) {
	if !c.Enabled() {
		return nil, errors.New("ai: client is not configured")
	}
	prompt := fmt.Sprintf(
		"单词：%s\n中文释义：%s\n请写 2 条适合孩子阅读的英文例句，附中文翻译。"+
			"每条句子必须包含这个单词或它的词形（%s）。"+
			"只输出 JSON 数组，格式：[{\"en\":\"...\",\"zh\":\"...\"}]",
		word, translation, strings.Join(forms, ", "))

	payload, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.4,
	})
	if err != nil {
		return nil, fmt.Errorf("ai: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return nil, fmt.Errorf("ai: service returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("ai: read response: %w", err)
	}
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("ai: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return nil, errors.New(emptyContentErrMsg)
	}
	return parseExamples(parsed.Choices[0].Message.Content, forms)
}

// parseExamples pulls the JSON array out of a model reply, which is often
// wrapped in prose or a code fence.
func parseExamples(content string, forms []string) ([]Example, error) {
	var raw []Example
	if start, end := strings.Index(content, "["), strings.LastIndex(content, "]"); start >= 0 && end > start {
		if err := json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil {
			raw = nil
		}
	}
	if raw == nil {
		var wrapper struct {
			Examples []Example `json:"examples"`
		}
		if start, end := strings.Index(content, "{"), strings.LastIndex(content, "}"); start >= 0 && end > start {
			if err := json.Unmarshal([]byte(content[start:end+1]), &wrapper); err == nil {
				raw = wrapper.Examples
			}
		}
	}

	lowerForms := make([]string, 0, len(forms))
	for _, f := range forms {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			lowerForms = append(lowerForms, f)
		}
	}

	out := make([]Example, 0, maxExamples)
	mentionsWord := false
	for _, ex := range raw {
		en := strings.TrimSpace(ex.EN)
		zh := strings.TrimSpace(ex.ZH)
		if en == "" || zh == "" {
			continue
		}
		if utf8.RuneCountInString(en) > maxSentenceRunes || utf8.RuneCountInString(zh) > maxSentenceRunes {
			continue
		}
		out = append(out, Example{EN: en, ZH: zh})
		if containsAnyForm(en, lowerForms) {
			mentionsWord = true
		}
		if len(out) == maxExamples {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("ai: response has no usable examples")
	}
	if !mentionsWord {
		return nil, errors.New("ai: examples do not use the requested word")
	}
	return out, nil
}

func containsAnyForm(sentence string, lowerForms []string) bool {
	lower := strings.ToLower(sentence)
	for _, f := range lowerForms {
		if strings.Contains(lower, f) {
			return true
		}
	}
	return false
}
