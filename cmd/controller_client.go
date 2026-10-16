package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func postAccepted(path string, payload any) (string, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBaseURL, "/")+path, body)
	if err != nil {
		return "", err
	}
	if apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(respBody))
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("unexpected status %s: %s", resp.Status, text)
	}
	return text, nil
}
