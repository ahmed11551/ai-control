package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	base   string
	http   *http.Client
	pause  time.Duration
	portal string
}

type apiError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func NewClient(webhook string) (*Client, error) {
	base, portal, err := normalizeWebhook(webhook)
	if err != nil {
		return nil, err
	}
	return &Client{
		base:   base,
		portal: portal,
		http:   &http.Client{Timeout: 90 * time.Second},
		pause:  450 * time.Millisecond,
	}, nil
}

func (c *Client) Portal() string { return c.portal }

func normalizeWebhook(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("укажите входящий вебхук Bitrix24")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("вебхук должен быть URL вида https://portal.bitrix24.ru/rest/1/xxxxx/")
	}
	if !strings.Contains(u.Path, "/rest/") {
		return "", "", fmt.Errorf("это не входящий вебхук: в пути нет /rest/")
	}
	path := u.Path
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), u.Host, nil
}

func (c *Client) Call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.pause):
			}
		}
		raw, err := c.callOnce(ctx, method, params)
		if err == nil {
			return raw, nil
		}
		last = err
		if !retryable(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 1500 * time.Millisecond):
		}
	}
	return nil, last
}

func (c *Client) Batch(ctx context.Context, cmds map[string]string) (map[string]json.RawMessage, error) {
	if len(cmds) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	raw, err := c.Call(ctx, "batch", map[string]any{"halt": 0, "cmd": cmds})
	if err != nil {
		return nil, err
	}
	var nested struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &nested) == nil && nested.Result != nil {
		return nested.Result, nil
	}
	var flat map[string]json.RawMessage
	if json.Unmarshal(raw, &flat) == nil {
		return flat, nil
	}
	return nil, fmt.Errorf("batch: не разобрать ответ")
}

func (c *Client) callOnce(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	body, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var apiErr apiError
	_ = json.Unmarshal(payload, &apiErr)
	if apiErr.Code != "" {
		return nil, fmt.Errorf("%s: %s", apiErr.Code, apiErr.Description)
	}
	var wrap struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(payload, &wrap); err != nil {
		return nil, fmt.Errorf("bitrix json: %w", err)
	}
	if wrap.Result == nil {
		return json.RawMessage("null"), nil
	}
	return wrap.Result, nil
}

func retryable(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "query_limit_exceeded") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "internal_server_error") ||
		strings.Contains(s, "forcibly closed") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "wsarecv") ||
		strings.Contains(s, "eof") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection refused")
}

type listPage struct {
	Result json.RawMessage `json:"result"`
	Next   json.RawMessage `json:"next"`
	Total  int             `json:"total"`
}

func (c *Client) List(ctx context.Context, method string, params map[string]any) ([]map[string]any, error) {
	items, _, err := c.list(ctx, method, params, false)
	return items, err
}

func (c *Client) ListAfter(ctx context.Context, method string, params map[string]any, afterID int64) ([]map[string]any, int, error) {
	p := cloneMap(params)
	filter, _ := p["filter"].(map[string]any)
	if filter == nil {
		filter = map[string]any{}
	}
	if afterID > 0 {
		filter[">ID"] = afterID
	}
	p["filter"] = filter
	p["start"] = 0
	raw, _, total, err := c.callList(ctx, method, p)
	if err != nil {
		return nil, total, err
	}
	return unwrapRows(method, raw), total, nil
}

func (c *Client) EachAfter(ctx context.Context, method string, params map[string]any, afterID int64, fn func(page []map[string]any, total int) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, total, err := c.ListAfter(ctx, method, params, afterID)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		if err := fn(page, total); err != nil {
			return err
		}
		next := lastRowID(page, afterID)
		if next <= afterID {
			return nil
		}
		afterID = next
	}
}

func lastRowID(rows []map[string]any, afterID int64) int64 {
	for _, row := range rows {
		if id, err := strconv.ParseInt(rowString(row, "ID", "id"), 10, 64); err == nil && id > afterID {
			afterID = id
		}
	}
	return afterID
}

func (c *Client) EachPage(ctx context.Context, method string, params map[string]any, start int, fn func(rows []map[string]any, total, next int) error) error {
	if params == nil {
		params = map[string]any{}
	}
	if start < 0 {
		start = 0
	}
	for {
		p := cloneMap(params)
		p["start"] = start
		raw, nextRaw, pageTotal, err := c.callList(ctx, method, p)
		if err != nil {
			return err
		}
		rows := unwrapRows(method, raw)
		n := nextStart(nextRaw)
		if err := fn(rows, pageTotal, n); err != nil {
			return err
		}
		if n <= 0 || len(rows) == 0 {
			return nil
		}
		start = n
	}
}

func (c *Client) Count(ctx context.Context, method string, params map[string]any) (int, error) {
	_, total, err := c.list(ctx, method, params, true)
	return total, err
}

func (c *Client) list(ctx context.Context, method string, params map[string]any, firstOnly bool) ([]map[string]any, int, error) {
	if params == nil {
		params = map[string]any{}
	}
	start := 0
	var all []map[string]any
	total := 0
	for {
		p := cloneMap(params)
		p["start"] = start
		raw, next, pageTotal, err := c.callList(ctx, method, p)
		if err != nil {
			return all, total, err
		}
		if pageTotal > total {
			total = pageTotal
		}
		rows := unwrapRows(method, raw)
		all = append(all, rows...)
		if firstOnly {
			if total == 0 {
				total = len(rows)
			}
			return all, total, nil
		}
		n := nextStart(next)
		if n <= 0 || len(rows) == 0 {
			break
		}
		start = n
	}
	if total == 0 {
		total = len(all)
	}
	return all, total, nil
}

func (c *Client) callList(ctx context.Context, method string, params map[string]any) (json.RawMessage, json.RawMessage, int, error) {
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		select {
		case <-ctx.Done():
			return nil, nil, 0, ctx.Err()
		case <-time.After(c.pause):
		}
		body, _ := json.Marshal(params)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, bytes.NewReader(body))
		if err != nil {
			return nil, nil, 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}
		payload, err := io.ReadAll(io.LimitReader(res.Body, 12<<20))
		res.Body.Close()
		if err != nil {
			last = err
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		var apiErr apiError
		_ = json.Unmarshal(payload, &apiErr)
		if apiErr.Code != "" {
			err = fmt.Errorf("%s: %s", apiErr.Code, apiErr.Description)
			if retryable(err) {
				last = err
				time.Sleep(time.Duration(attempt+1) * 1500 * time.Millisecond)
				continue
			}
			return nil, nil, 0, err
		}
		var page listPage
		if err := json.Unmarshal(payload, &page); err != nil {
			return nil, nil, 0, err
		}
		return page.Result, page.Next, page.Total, nil
	}
	return nil, nil, 0, last
}

func unwrapRows(method string, raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	for _, key := range []string{"tasks", "items", "result"} {
		if v, ok := obj[key]; ok {
			b, _ := json.Marshal(v)
			var inner []map[string]any
			if json.Unmarshal(b, &inner) == nil {
				return inner
			}
		}
	}
	if method == "tasks.task.list" {
		return nil
	}
	return nil
}

func nextStart(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	return 0
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
