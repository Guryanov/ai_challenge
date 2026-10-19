package agent

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIClient — клиент для вызова внешнего API.
type APIClient interface {
	Call(payload []byte) ([]byte, error)
}

// httpAPIClient — реализация APIClient поверх HTTP.
type httpAPIClient struct {
	url      string
	apiKey   string
	authType string
	timeout  time.Duration
	client   *http.Client
}

// NewHTTPClient создаёт HTTP-клиент для внешнего API.
func NewHTTPClient(externalAPI, apiKey, authType string, timeout time.Duration) APIClient {
	if timeout == 0 {
		timeout = 360 * time.Second
	}
	return &httpAPIClient{
		url:      externalAPI,
		apiKey:   apiKey,
		authType: authType,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
	}
}

func (c *httpAPIClient) Call(payload []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка вызова внешнего API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("внешний API вернул статус %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *httpAPIClient) setAuthHeader(req *http.Request) {
	if c.apiKey == "" {
		return
	}

	switch c.authType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	case "x-api-key":
		req.Header.Set("X-API-Key", c.apiKey)
	case "api-key":
		req.Header.Set("Api-Key", c.apiKey)
	}
}
