package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxResponseBytes = 64 << 10

var (
	ErrNotFound = errors.New("access request not found")
	ErrConflict = errors.New("access request conflict")

	bearerToken = regexp.MustCompile(`^[a-zA-Z0-9._~+/-]+=*$`)
)

type Ticket struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Hostname() == "" || parsed.User != nil || parsed.ForceQuery ||
		parsed.RawQuery != "" || strings.Contains(baseURL, "#") {
		return nil, errors.New("invalid access service URL")
	}
	if !bearerToken.MatchString(token) {
		return nil, errors.New("invalid access service token")
	}

	return &Client{
		baseURL: parsed,
		token:   token,
		http: &http.Client{
			Timeout: 3 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) Create(ctx context.Context, key string, payload []byte) (Ticket, error) {
	if strings.TrimSpace(key) == "" {
		return Ticket{}, errors.New("missing access request key")
	}
	return c.do(ctx, http.MethodPost, "/requests", key, payload)
}

func (c *Client) Lookup(ctx context.Context, key string) (Ticket, error) {
	if strings.TrimSpace(key) == "" {
		return Ticket{}, errors.New("missing access request key")
	}
	return c.do(ctx, http.MethodGet, "/requests/by-key/"+escapeSegment(key), "", nil)
}

func (c *Client) Status(ctx context.Context, id string) (Ticket, error) {
	if strings.TrimSpace(id) == "" {
		return Ticket{}, errors.New("missing access request ID")
	}
	return c.do(ctx, http.MethodGet, "/requests/"+escapeSegment(id), "", nil)
}

func escapeSegment(segment string) string {
	if segment == "." || segment == ".." {
		return strings.ReplaceAll(segment, ".", "%2E")
	}
	return url.PathEscape(segment)
}

func (c *Client) do(ctx context.Context, method, escapedPath, key string, payload []byte) (Ticket, error) {
	endpoint := *c.baseURL
	endpoint.RawPath = strings.TrimSuffix(endpoint.EscapedPath(), "/") + escapedPath
	endpoint.Path, _ = url.PathUnescape(endpoint.RawPath)

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return Ticket{}, errors.New("could not construct access service request")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		// The worker checks the key before replaying an ambiguous POST.
		request.GetBody = nil
	}

	response, err := c.http.Do(request)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return Ticket{}, context.Canceled
		case errors.Is(err, context.DeadlineExceeded):
			return Ticket{}, context.DeadlineExceeded
		default:
			return Ticket{}, errors.New("access service request failed")
		}
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusNotFound:
		return Ticket{}, ErrNotFound
	case http.StatusConflict:
		return Ticket{}, ErrConflict
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Ticket{}, fmt.Errorf("access service returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return Ticket{}, errors.New("could not read access service response")
	}
	if len(body) > maxResponseBytes {
		return Ticket{}, errors.New("access service response exceeds size limit")
	}

	var ticket Ticket
	if err := json.Unmarshal(body, &ticket); err != nil || strings.TrimSpace(ticket.ID) == "" {
		return Ticket{}, errors.New("invalid access service response")
	}
	switch ticket.Status {
	case "pending_approval", "approved", "rejected":
		return ticket, nil
	default:
		return Ticket{}, errors.New("invalid access request status")
	}
}
