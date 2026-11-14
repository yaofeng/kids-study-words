package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// Base is embedded in every page's template data so the layout can render
// navigation, CSRF tokens, and flash messages.
type Base struct {
	Title           string
	Username        string
	Authorizer      string
	User            store.User
	CSRF            string
	Active          string
	Notebooks       []store.Notebook
	DefaultNotebook *store.Notebook
	Flash           string
	Error           string
	Version         string
	AssetVersion    string
}

// StarState is one of the five star positions.
type StarState struct {
	Fill string // "full", "half", or "empty"
}

// Stars converts a 1–10 proficiency into five star states.
func Stars(level int) []StarState {
	if level < 0 {
		level = 0
	}
	if level > 10 {
		level = 10
	}
	out := make([]StarState, 5)
	for i := 0; i < 5; i++ {
		switch {
		case level >= (i+1)*2:
			out[i] = StarState{Fill: "full"}
		case level == i*2+1:
			out[i] = StarState{Fill: "half"}
		default:
			out[i] = StarState{Fill: "empty"}
		}
	}
	return out
}

// StarLabel is the accessible label for a proficiency, e.g. "熟练度 2.5 / 5".
func StarLabel(level int) string {
	if level < 0 {
		level = 0
	}
	if level > 10 {
		level = 10
	}
	return fmt.Sprintf("熟练度 %.1f / 5", float64(level)/2)
}

// FormatTime renders a timestamp as local wall-clock time.
func (s *Server) FormatTime(t time.Time) string { return t.In(s.loc).Format("15:04") }

// FormatDate renders a timestamp as an ISO-style local date.
func (s *Server) FormatDate(t time.Time) string { return t.In(s.loc).Format("2006-01-02") }

var weekdayNames = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// Weekday renders a timestamp's local Chinese weekday.
func (s *Server) Weekday(t time.Time) string { return weekdayNames[t.In(s.loc).Weekday()] }

// renderer parses templates on first use and caches them by name.
type renderer struct {
	fsys  fs.FS
	funcs template.FuncMap

	mu    sync.Mutex
	cache map[string]*template.Template
}

func newRenderer(fsys fs.FS, funcs template.FuncMap) *renderer {
	return &renderer{fsys: fsys, funcs: funcs, cache: make(map[string]*template.Template)}
}

// load parses layout.html (when withLayout) plus partials and, when it
// exists, the named file. Fragments may live entirely in partials.html.
func (r *renderer) load(name string, withLayout bool) (*template.Template, error) {
	key := name
	files := []string{"partials.html"}
	if _, err := fs.Stat(r.fsys, name+".html"); err == nil {
		files = append(files, name+".html")
	}
	if withLayout {
		key = "page:" + name
		files = append([]string{"layout.html"}, files...)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if tmpl, ok := r.cache[key]; ok {
		return tmpl, nil
	}
	tmpl, err := template.New("root").Funcs(r.funcs).ParseFS(r.fsys, files...)
	if err != nil {
		return nil, fmt.Errorf("parse templates %v: %w", files, err)
	}
	r.cache[key] = tmpl
	return tmpl, nil
}

// page renders a full page through layout.html.
func (r *renderer) page(w http.ResponseWriter, status int, name string, data any) error {
	tmpl, err := r.load(name, true)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		return fmt.Errorf("execute page %s: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// fragment renders a named template defined in partials.html or <name>.html.
func (r *renderer) fragment(w http.ResponseWriter, status int, name string, data any) error {
	tmpl, err := r.load(name, false)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("execute fragment %s: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// fail logs an internal error and shows the error page.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	s.renderError(w, r, http.StatusInternalServerError, "哎呀，出了点问题，请稍后再试。")
}

// renderError shows the friendly error page.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	data := Base{
		Title:        http.StatusText(status),
		CSRF:         csrfFrom(r),
		Error:        msg,
		Version:      s.version,
		AssetVersion: s.assetVersion,
	}
	data.User = currentUser(r)
	if err := s.renderer.page(w, status, "error", data); err != nil {
		slog.Error("render error page", "err", err)
		http.Error(w, msg, status)
	}
}

// setFlash stores a one-shot message for the next page load.
func setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    url.QueryEscape(msg),
		Path:     "/",
		MaxAge:   30,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// popFlash reads and clears the flash cookie.
func popFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(flashCookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookieName, Value: "", Path: "/", MaxAge: -1})
	msg, err := url.QueryUnescape(c.Value)
	if err != nil {
		return c.Value
	}
	return msg
}

// firstLine returns the first non-empty line of a multi-line translation.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// Lines splits a multi-line translation into trimmed non-empty lines.
func Lines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

var tagLabels = map[string]string{
	"zk":   "中考",
	"gk":   "高考",
	"cet4": "CET4",
	"cet6": "CET6",
}

// Tags translates ECDICT tag codes into Chinese exam labels.
func Tags(tag string) []string {
	var out []string
	for _, code := range strings.Fields(tag) {
		if label, ok := tagLabels[strings.ToLower(code)]; ok {
			out = append(out, label)
		}
	}
	return out
}
