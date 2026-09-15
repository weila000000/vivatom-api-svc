package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vivatom-api-svc/internal/domain"
)

type Client struct {
	url        string
	token      string
	httpClient *http.Client
}

type RejectedError struct{}

func (RejectedError) Error() string         { return "build rejected" }
func (RejectedError) CompileRejected() bool { return true }

func NewClient(url, token string, timeout time.Duration) *Client {
	return &Client{url: strings.TrimRight(url, "/"), token: token, httpClient: &http.Client{Timeout: timeout}}
}

func (c *Client) Compile(ctx context.Context, snapshot domain.ProjectSnapshot) (domain.BuildVerification, error) {
	payload, err := json.Marshal(struct {
		Snapshot domain.ProjectSnapshot `json:"snapshot"`
	}{Snapshot: snapshot})
	if err != nil {
		return domain.BuildVerification{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/compile", bytes.NewReader(payload))
	if err != nil {
		return domain.BuildVerification{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Vivatom-Builder-Token", c.token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return domain.BuildVerification{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnprocessableEntity {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return domain.BuildVerification{}, RejectedError{}
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return domain.BuildVerification{}, fmt.Errorf("builder returned status %d", response.StatusCode)
	}
	var envelope struct {
		Data domain.BuildVerification `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err = decoder.Decode(&envelope); err != nil || envelope.Data.Toolchain == "" || envelope.Data.DurationMS < 0 {
		return domain.BuildVerification{}, fmt.Errorf("invalid builder response")
	}
	return envelope.Data, nil
}
