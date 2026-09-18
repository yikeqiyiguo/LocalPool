package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTP 客户端封装，向 Server 的 /agent 端点发起请求
type client struct {
	base   string
	secret string
	hc     *http.Client
}

func newClient(server, secret string) *client {
	return &client{
		base:   strings.TrimRight(server, "/"),
		secret: secret,
		hc:     &http.Client{Timeout: 15 * time.Second},
	}
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *client) postJSON(ctx context.Context, path string, req, out any) (int, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		httpReq.Header.Set("X-Agent-Key", c.secret)
	}
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusConflict {
		return resp.StatusCode, fmt.Errorf("server requires re-register")
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("server error %d: %s", resp.StatusCode, string(raw))
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return resp.StatusCode, fmt.Errorf("bad response: %w", err)
	}
	if env.Code != 0 {
		return resp.StatusCode, fmt.Errorf("api error: %s", env.Msg)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}
