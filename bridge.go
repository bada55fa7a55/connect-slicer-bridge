package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"
)

const octoPrintVersion = "1.10.0"

type bridge struct {
	connect        *connectClient
	storage        string
	printerStorage string
	keepFolder     string
	log            *slog.Logger
}

func (b *bridge) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{printer}/api/version", b.version)
	mux.HandleFunc("POST /{printer}/api/files/local", b.uploadFile)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

func (b *bridge) version(w http.ResponseWriter, r *http.Request) {
	token, ok := apiKey(w, r)
	if !ok {
		return
	}
	d, err := b.connect.findDevice(r.Context(), token, r.PathValue("printer"))
	if err != nil {
		b.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"api":    "0.1",
		"server": octoPrintVersion,
		"text":   fmt.Sprintf("OctoPrint %s (connect-slicer-bridge for %s)", octoPrintVersion, d.Name),
	})
}

type uploadForm struct {
	print    bool
	folder   string
	filename string
	source   string
}

func (b *bridge) uploadFile(w http.ResponseWriter, r *http.Request) {
	token, ok := apiKey(w, r)
	if !ok {
		return
	}
	d, err := b.connect.findDevice(r.Context(), token, r.PathValue("printer"))
	if err != nil {
		b.fail(w, r, err)
		return
	}

	form, err := b.readUpload(r, token)
	if err != nil {
		b.fail(w, r, err)
		return
	}

	dest := path.Join(form.folder, form.filename)
	f := printerFile{
		Path:    path.Join(b.printerStorage, dest),
		Storage: b.storage,
		Source:  form.source,
		Replace: true,
	}
	if form.print {
		err = b.connect.printFile(r.Context(), token, d.ID, f)
	} else {
		err = b.connect.sendFile(r.Context(), token, d.ID, f)
	}
	if err != nil {
		b.fail(w, r, err)
		return
	}

	b.log.Info("uploaded", "printer", d.Name, "file", f.Path, "print", form.print)
	writeJSON(w, http.StatusCreated, map[string]any{
		"done": true,
		"files": map[string]any{
			"local": map[string]string{"name": form.filename, "path": dest, "origin": "local"},
		},
	})
}

func (b *bridge) readUpload(r *http.Request, token string) (uploadForm, error) {
	var form uploadForm
	mr, err := r.MultipartReader()
	if err != nil {
		return form, badRequest("expected a multipart/form-data upload")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return form, badRequest("malformed multipart body: " + err.Error())
		}
		switch part.FormName() {
		case "print":
			form.print, err = readBool(part)
		case "path":
			form.folder, err = readFolder(part)
		case "file":
			err = b.storeFile(r, token, part, &form)
		}
		part.Close()
		if err != nil {
			return form, err
		}
	}
	if form.source == "" {
		return form, badRequest("the upload carries no file part")
	}
	return form, nil
}

func (b *bridge) storeFile(r *http.Request, token string, part *multipart.Part, form *uploadForm) error {
	name := path.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))
	if name == "" || name == "." || name == "/" || name == ".." {
		return badRequest("the file part has no usable file name")
	}
	form.filename = name

	keep := b.keepFolder != ""
	target := name
	if keep {
		target = path.Join(b.keepFolder, name)
	}
	saved, err := b.connect.upload(r.Context(), token, b.storage, target, !keep, part)
	if err != nil {
		return err
	}
	form.source = saved.Path
	return nil
}

func readBool(part *multipart.Part) (bool, error) {
	raw, err := io.ReadAll(io.LimitReader(part, 16))
	if err != nil {
		return false, err
	}
	v, err := strconv.ParseBool(strings.TrimSpace(string(raw)))
	if err != nil {
		return false, badRequest("print must be true or false")
	}
	return v, nil
}

func readFolder(part *multipart.Part) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(part, 1024))
	if err != nil {
		return "", err
	}
	folder := strings.Trim(strings.ReplaceAll(strings.TrimSpace(string(raw)), `\`, "/"), "/")
	if folder == "" {
		return "", nil
	}
	clean := path.Clean(folder)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", badRequest("path must stay inside the printer's storage")
	}
	return clean, nil
}

func apiKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := strings.TrimSpace(r.Header.Get("X-Api-Key"))
	if token == "" {
		http.Error(w, "missing API key: put a Prusa Connect API credential in the slicer's API key field", http.StatusUnauthorized)
		return "", false
	}
	return token, true
}

func badRequest(msg string) error {
	return &bridgeError{Status: http.StatusBadRequest, Msg: msg}
}

func (b *bridge) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadGateway
	msg := "Cannot reach Prusa Connect: " + err.Error()
	var bridgeErr *bridgeError
	var apiErr *apiError
	switch {
	case errors.As(err, &bridgeErr):
		status, msg = bridgeErr.Status, bridgeErr.Msg
	case errors.As(err, &apiErr):
		status, msg = apiErr.Status, apiErr.Error()
	}
	b.log.Warn("request failed", "method", r.Method, "path", r.URL.Path, "status", status, "error", msg)
	http.Error(w, msg, status)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
