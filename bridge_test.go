package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const testToken = "secret-token"

type fakeConnect struct {
	mu       sync.Mutex
	devices  []device
	uploads  map[string][]byte
	sent     []printerFile
	printed  []printerFile
	refuse   bool
	lastAuth string
}

func (f *fakeConnect) handler() http.Handler {
	mux := http.NewServeMux()
	problem := func(w http.ResponseWriter, status int, code, title string) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"status": status, "code": code, "title": title})
	}
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.lastAuth = r.Header.Get("Authorization")
			f.mu.Unlock()
			if r.Header.Get("Authorization") != "Bearer "+testToken {
				problem(w, 401, "core.auth/UNAUTHENTICATED", "Sign in first")
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/devices/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		for _, d := range f.devices {
			if d.ID == r.PathValue("id") {
				json.NewEncoder(w).Encode(d)
				return
			}
		}
		problem(w, 404, "core.device/NOT_FOUND", "No such device")
	}))
	mux.HandleFunc("GET /api/v1/devices", auth(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(devicePage{Items: f.devices, Total: len(f.devices)})
	}))
	mux.HandleFunc("PUT /api/v1/storages/local/content", auth(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p := r.URL.Query().Get("path")
		if r.URL.Query().Get("temporary") == "true" {
			p = ".tmp/abc/" + p
		}
		f.mu.Lock()
		f.uploads[p] = body
		f.mu.Unlock()
		json.NewEncoder(w).Encode(savedObject{Path: p, Bytes: int64(len(body))})
	}))
	mux.HandleFunc("POST /api/v1/devices/{id}/files", auth(func(w http.ResponseWriter, r *http.Request) {
		var pf printerFile
		json.NewDecoder(r.Body).Decode(&pf)
		f.mu.Lock()
		f.sent = append(f.sent, pf)
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"path": pf.Path})
	}))
	mux.HandleFunc("POST /api/v1/devices/{id}/print", auth(func(w http.ResponseWriter, r *http.Request) {
		var pf printerFile
		json.NewDecoder(r.Body).Decode(&pf)
		f.mu.Lock()
		f.printed = append(f.printed, pf)
		f.mu.Unlock()
		res := printResult{Command: printerResult{OK: !f.refuse}}
		if f.refuse {
			res.Command.FaultCode = "core.printer/BUSY"
			res.Command.MachineReason = "printer is busy"
		}
		json.NewEncoder(w).Encode(res)
	}))
	return mux
}

func setup(t *testing.T, opts ...func(*bridge)) (*fakeConnect, *httptest.Server) {
	t.Helper()
	fake := &fakeConnect{
		devices: []device{
			{ID: "9f2a4c18", Name: "MK4S Workshop", Model: "MK4S", Connected: true},
			{ID: "1b2c3d4e", Name: "Core One", Model: "COREONE", Connected: true},
		},
		uploads: map[string][]byte{},
	}
	connect := httptest.NewServer(fake.handler())
	t.Cleanup(connect.Close)
	base, _ := url.Parse(connect.URL)
	b := &bridge{
		connect:        &connectClient{base: base, http: connect.Client()},
		storage:        "local",
		printerStorage: "usb",
		log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(b)
	}
	srv := httptest.NewServer(b.routes())
	t.Cleanup(srv.Close)
	return fake, srv
}

func get(t *testing.T, url, key string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

type field struct{ name, value string }

func upload(t *testing.T, url, key, filename string, content []byte, fields ...field) (*http.Response, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range fields {
		mw.WriteField(f.name, f.value)
	}
	if filename != "" {
		fw, _ := mw.CreateFormFile("file", filename)
		fw.Write(content)
	}
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Api-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestVersionSatisfiesPrusaSlicer(t *testing.T) {
	_, srv := setup(t)
	for _, ref := range []string{"9f2a4c18"} {
		resp, body := get(t, srv.URL+"/"+ref+"/api/version", testToken)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d: %s", ref, resp.StatusCode, body)
		}
		var v map[string]string
		json.Unmarshal([]byte(body), &v)
		if !strings.HasPrefix(v["text"], "OctoPrint") {
			t.Fatalf("%s: text %q does not start with OctoPrint", ref, v["text"])
		}
	}
}

func TestVersionErrors(t *testing.T) {
	_, srv := setup(t)
	cases := []struct {
		name, ref, key string
		status         int
	}{
		{"no key", "9f2a4c18", "", 401},
		{"bad key", "9f2a4c18", "wrong", 401},
		{"unknown printer", "xl", testToken, 404},
		{"name is not an id", "mk4s-workshop", testToken, 404},
	}
	for _, c := range cases {
		resp, body := get(t, srv.URL+"/"+c.ref+"/api/version", c.key)
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d: %s", c.name, resp.StatusCode, c.status, body)
		}
	}
}

func TestUnknownPrinterListsValidNames(t *testing.T) {
	_, srv := setup(t)
	resp, body := get(t, srv.URL+"/xl/api/version", testToken)
	if resp.StatusCode != 404 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{"MK4S Workshop: 9f2a4c18", "Core One: 1b2c3d4e"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q lacks %q", body, want)
		}
	}
}

func TestUploadSendsToPrinter(t *testing.T) {
	fake, srv := setup(t)
	content := []byte("GCDE binary gcode")
	resp, body := upload(t, srv.URL+"/9f2a4c18/api/files/local", testToken, "bracket.bgcode", content,
		field{"print", "false"}, field{"path", "parts"})
	if resp.StatusCode != 201 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if got := fake.uploads["bracket.bgcode"]; !bytes.Equal(got, content) {
		t.Fatalf("stored %q, want %q", got, content)
	}
	want := printerFile{Path: "usb/parts/bracket.bgcode", Storage: "local", Source: "bracket.bgcode", Replace: true}
	if len(fake.sent) != 1 || fake.sent[0] != want {
		t.Fatalf("sent %+v, want %+v", fake.sent, want)
	}
	if len(fake.printed) != 0 {
		t.Fatalf("printed %+v, want nothing", fake.printed)
	}
	if fake.lastAuth != "Bearer "+testToken {
		t.Fatalf("auth %q", fake.lastAuth)
	}
}

func TestUploadAndPrint(t *testing.T) {
	fake, srv := setup(t)
	resp, body := upload(t, srv.URL+"/9f2a4c18/api/files/local", testToken, "bracket.bgcode", []byte("x"),
		field{"print", "true"}, field{"path", ""})
	if resp.StatusCode != 201 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if len(fake.printed) != 1 || fake.printed[0].Path != "usb/bracket.bgcode" {
		t.Fatalf("printed %+v", fake.printed)
	}
}

func TestPrintRefusedIsReported(t *testing.T) {
	fake, srv := setup(t)
	fake.refuse = true
	resp, body := upload(t, srv.URL+"/9f2a4c18/api/files/local", testToken, "bracket.bgcode", []byte("x"), field{"print", "true"})
	if resp.StatusCode != 409 || !strings.Contains(body, "printer is busy") {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
}

func TestUploadFolder(t *testing.T) {
	fake, srv := setup(t, func(b *bridge) { b.folder = "slicer" })
	resp, body := upload(t, srv.URL+"/9f2a4c18/api/files/local", testToken, "bracket.bgcode", []byte("x"), field{"print", "false"})
	if resp.StatusCode != 201 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if _, ok := fake.uploads["slicer/bracket.bgcode"]; !ok {
		t.Fatalf("uploads %v", fake.uploads)
	}
	if fake.sent[0].Source != "slicer/bracket.bgcode" {
		t.Fatalf("source %q", fake.sent[0].Source)
	}
}

func TestTemporaryUploads(t *testing.T) {
	fake, srv := setup(t, func(b *bridge) { b.temporary = true })
	resp, body := upload(t, srv.URL+"/9f2a4c18/api/files/local", testToken, "bracket.bgcode", []byte("x"), field{"print", "false"})
	if resp.StatusCode != 201 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if _, ok := fake.uploads[".tmp/abc/bracket.bgcode"]; !ok {
		t.Fatalf("uploads %v", fake.uploads)
	}
	if fake.sent[0].Source != ".tmp/abc/bracket.bgcode" {
		t.Fatalf("source %q", fake.sent[0].Source)
	}
}

func TestUploadRejectsBadInput(t *testing.T) {
	_, srv := setup(t)
	cases := []struct {
		name     string
		filename string
		fields   []field
	}{
		{"no file", "", []field{{"print", "false"}}},
		{"escaping path", "a.bgcode", []field{{"path", "../../etc"}}},
		{"bad print flag", "a.bgcode", []field{{"print", "maybe"}}},
	}
	for _, c := range cases {
		resp, body := upload(t, srv.URL+"/9f2a4c18/api/files/local", testToken, c.filename, []byte("x"), c.fields...)
		if resp.StatusCode != 400 {
			t.Errorf("%s: status %d: %s", c.name, resp.StatusCode, body)
		}
	}
}

func TestWindowsFileNameIsReducedToBase(t *testing.T) {
	fake, srv := setup(t)
	resp, body := upload(t, srv.URL+"/9f2a4c18/api/files/local", testToken, `C:\Users\me\..\part.bgcode`, []byte("x"))
	if resp.StatusCode != 201 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if fake.sent[0].Path != "usb/part.bgcode" {
		t.Fatalf("path %q", fake.sent[0].Path)
	}
}
