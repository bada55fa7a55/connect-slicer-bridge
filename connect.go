package main

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
)

type connectClient struct {
	base *url.URL
	http *http.Client
}

type apiError struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (e *apiError) Error() string {
	msg := e.Title
	if e.Detail != "" && e.Detail != e.Title {
		msg += ": " + e.Detail
	}
	if e.Code != "" {
		msg += " (" + e.Code + ")"
	}
	return fmt.Sprintf("Prusa Connect answered %d: %s", e.Status, msg)
}

type device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Connected bool   `json:"connected"`
}

type devicePage struct {
	Items []device `json:"items"`
	Total int      `json:"total"`
}

type savedObject struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type printerFile struct {
	Path    string `json:"path"`
	Storage string `json:"storage"`
	Source  string `json:"source"`
	Replace bool   `json:"replace"`
}

type printerResult struct {
	OK            bool   `json:"ok"`
	FaultCode     string `json:"fault_code"`
	MachineReason string `json:"machine_reason"`
}

type printResult struct {
	Command printerResult `json:"command"`
}

func (c *connectClient) do(ctx context.Context, token, method, path string, query url.Values, body io.Reader, contentType string, out any) error {
	u := c.base.JoinPath(path)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		apiErr := &apiError{}
		if json.Unmarshal(raw, apiErr) != nil || apiErr.Title == "" {
			apiErr.Title = strings.TrimSpace(string(raw))
		}
		apiErr.Status = resp.StatusCode
		return apiErr
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *connectClient) getDevice(ctx context.Context, token, id string) (device, error) {
	var d device
	err := c.do(ctx, token, http.MethodGet, "api/v1/devices/"+url.PathEscape(id), nil, nil, "", &d)
	return d, err
}

func (c *connectClient) listDevices(ctx context.Context, token string) ([]device, error) {
	var page devicePage
	err := c.do(ctx, token, http.MethodGet, "api/v1/devices", url.Values{"limit": {"1000"}}, nil, "", &page)
	return page.Items, err
}

func (c *connectClient) findDevice(ctx context.Context, token, id string) (device, error) {
	d, err := c.getDevice(ctx, token, id)
	if err == nil {
		return d, nil
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || (apiErr.Status != http.StatusNotFound && apiErr.Status != http.StatusBadRequest && apiErr.Status != http.StatusUnprocessableEntity) {
		return device{}, err
	}

	devices, err := c.listDevices(ctx, token)
	if err != nil {
		return device{}, err
	}
	return device{}, &apiError{Status: http.StatusNotFound, Title: fmt.Sprintf("no printer with the UUID %q is visible to this API key", id), Detail: available(devices)}
}

func (c *connectClient) upload(ctx context.Context, token, storage, path string, temporary bool, body io.Reader) (savedObject, error) {
	q := url.Values{"path": {path}}
	if temporary {
		q.Set("temporary", "true")
	}
	var saved savedObject
	err := c.do(ctx, token, http.MethodPut, "api/v1/storages/"+url.PathEscape(storage)+"/content", q, body, "application/octet-stream", &saved)
	return saved, err
}

func (c *connectClient) sendFile(ctx context.Context, token, deviceID string, f printerFile) error {
	return c.postJSON(ctx, token, "api/v1/devices/"+url.PathEscape(deviceID)+"/files", f, nil)
}

func (c *connectClient) printFile(ctx context.Context, token, deviceID string, f printerFile) error {
	var res printResult
	if err := c.postJSON(ctx, token, "api/v1/devices/"+url.PathEscape(deviceID)+"/print", f, &res); err != nil {
		return err
	}
	if !res.Command.OK {
		reason := res.Command.MachineReason
		if reason == "" {
			reason = res.Command.FaultCode
		}
		return &apiError{Status: http.StatusConflict, Title: "the file arrived but the printer refused to start it", Detail: reason}
	}
	return nil
}

func (c *connectClient) postJSON(ctx context.Context, token, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, token, http.MethodPost, path, nil, bytes.NewReader(body), "application/json", out)
}

func available(devices []device) string {
	if len(devices) == 0 {
		return "this API key sees no printers"
	}
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = fmt.Sprintf("%s: %s", d.Name, d.ID)
	}
	return "printers this key can see: " + strings.Join(names, ", ")
}
