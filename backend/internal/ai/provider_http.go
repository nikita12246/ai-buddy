package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxInputBytes    = 48 * 1024
	maxResponseBytes = 256 * 1024
)

func providerHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func prepareInput(ctx context.Context, input Input) (string, error) {
	if strings.TrimSpace(input.Question) == "" {
		return "", ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return "", requestError(ctx, err)
	}
	data, err := json.Marshal(input)
	if err != nil || len(data) > maxInputBytes {
		return "", ErrInvalidInput
	}
	return string(data), nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint, key, apiVersion string, payload any) ([]byte, error) {
	requestJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrConfiguration
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestJSON))
	if err != nil {
		return nil, ErrConfiguration
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	if apiVersion != "" {
		request.Header.Set("anthropic-version", apiVersion)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, requestError(ctx, err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return nil, ErrConfiguration
	case http.StatusPaymentRequired:
		return nil, ErrBalance
	default:
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return nil, ErrUnavailable
		}
		return nil, ErrInvalidResponse
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, requestError(ctx, err)
	}
	if len(data) > maxResponseBytes {
		return nil, ErrInvalidResponse
	}
	return data, nil
}

func requestError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	var timeout interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return ErrTimeout
	}
	return ErrUnavailable
}
