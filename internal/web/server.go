package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/yaofeng/kids-study-words/internal/ai"
	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/recall"
	"github.com/yaofeng/kids-study-words/internal/store"
	assets "github.com/yaofeng/kids-study-words/web"
)

// ExampleAI generates extra example sentences for a word.
type ExampleAI interface {
	Enabled() bool
	Model() string
	Host() string
	Examples(ctx context.Context, word, translation string, forms []string) ([]ai.Example, error)
}

// Options configures a Server.
type Options struct {
	Store        *store.Store
	Dict         Dictionary
	AI           ExampleAI
	Location     *time.Location
	Version      string
	CookieSecure bool
	Now          func() time.Time
}

// Server owns routing, rendering, and authentication state.
type Server struct {
	store        *store.Store
	dict         Dictionary
	ai           ExampleAI
	loc          *time.Location
	version      string
	cookieSecure bool
	now          func() time.Time
	limiter      *auth.LoginLimiter
	recall       *recall.Manager
	renderer     *renderer
	handler      http.Handler
}

// New builds a Server and its middleware chain.
func New(opts Options) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("web: Options.Store is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}

	staticFS, err := fs.Sub(assets.Static, "static")
	if err != nil {
		return nil, fmt.Errorf("static assets: %w", err)
	}
	tmplFS, err := fs.Sub(assets.Templates, "templates")
	if err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}

	s := &Server{
		store:        opts.Store,
		dict:         opts.Dict,
		ai:           opts.AI,
		loc:          opts.Location,
		version:      opts.Version,
		cookieSecure: opts.CookieSecure,
		now:          opts.Now,
		limiter:      auth.NewLoginLimiter(5, time.Minute, opts.Now),
		recall:       recall.NewManager(),
	}
	s.renderer = newRenderer(tmplFS, s.templateFuncs())

	mux := s.routes(staticFS)
	var h http.Handler = mux
	h = s.csrfMiddleware(h)
	h = s.sessionMiddleware(h)
	h = s.setupGateMiddleware(h)
	h = loggingMiddleware(h)
	h = securityHeaders(h)
	h = recoverMiddleware(h)
	s.handler = h
	return s, nil
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"Stars":      Stars,
		"StarLabel":  StarLabel,
		"FormatTime": s.FormatTime,
		"FormatDate": s.FormatDate,
		"Weekday":    s.Weekday,
		"FirstLine":  firstLine,
		"Lines":      Lines,
		"Tags":       Tags,
	}
}

func (s *Server) routes(staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /setup", s.handleSetupPage)
	mux.HandleFunc("POST /setup", s.handleSetupPost)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("POST /logout", s.handleLogout)

	mux.HandleFunc("GET /notebooks", s.handleNotebooksPage)
	mux.HandleFunc("POST /notebooks", s.handleCreateNotebook)
	mux.HandleFunc("POST /notebooks/{id}/rename", s.handleRenameNotebook)
	mux.HandleFunc("POST /notebooks/{id}/default", s.handleSetDefaultNotebook)
	mux.HandleFunc("POST /notebooks/{id}/delete", s.handleDeleteNotebook)
	mux.HandleFunc("GET /notebooks/{id}", s.handleNotebookDetail)
	mux.HandleFunc("GET /notebooks/{id}/words/{wordID}/edit", s.handleWordEditPage)
	mux.HandleFunc("POST /notebooks/{id}/words/{wordID}/edit", s.handleWordEditPost)
	mux.HandleFunc("POST /notebooks/{id}/words/{wordID}/delete", s.handleWordDelete)

	mux.HandleFunc("GET /lookup", s.handleLookupPage)
	mux.HandleFunc("GET /lookup/results", s.handleLookupResults)
	mux.HandleFunc("GET /lookup/examples", s.handleLookupExamples)
	mux.HandleFunc("POST /words", s.handleAddWord)

	mux.HandleFunc("GET /recall", s.handleRecallSelect)
	mux.HandleFunc("GET /recall/{id}", s.handleRecallPage)
	mux.HandleFunc("POST /recall/{id}/answer", s.handleRecallAnswer)
	mux.HandleFunc("POST /recall/{id}/next", s.handleRecallNext)
	mux.HandleFunc("POST /recall/{id}/end", s.handleRecallEnd)
	mux.HandleFunc("GET /recall/{id}/summary", s.handleRecallSummary)
	mux.HandleFunc("GET /recall/{id}/restart", s.handleRecallRestart)

	mux.HandleFunc("GET /records", s.handleRecords)

	mux.HandleFunc("GET /settings", s.handleSettingsPage)
	mux.HandleFunc("POST /settings/password", s.handleChangePassword)
	mux.HandleFunc("GET /export", s.handleExport)
	mux.HandleFunc("POST /import", s.handleImport)

	static := http.StripPrefix("/static/", http.FileServerFS(staticFS))
	mux.Handle("GET /static/", cacheControl(static, "max-age=3600"))

	mux.HandleFunc("GET /", s.handleNotFound)
	return mux
}

// cacheControl adds a Cache-Control header to successful static responses.
func cacheControl(next http.Handler, value string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/lookup", http.StatusSeeOther)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		slog.Error("health check failed", "err", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	HealthHandler(w, r)
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "页面不存在，检查一下网址吧。")
}

// pageData fills Base and reports failure through the error page.
func (s *Server) pageData(w http.ResponseWriter, r *http.Request, title, active string) (Base, bool) {
	data, err := s.baseData(w, r)
	if err != nil {
		s.fail(w, r, err)
		return Base{}, false
	}
	data.Title = title
	data.Active = active
	return data, true
}

// baseData assembles the data every page template needs.
func (s *Server) baseData(w http.ResponseWriter, r *http.Request) (Base, error) {
	data := Base{
		User:    currentUser(r),
		CSRF:    csrfFrom(r),
		Flash:   popFlash(w, r),
		Version: s.version,
	}
	if data.User.ID == 0 {
		return data, nil
	}
	notebooks, err := s.store.ListNotebooks(r.Context(), data.User.ID)
	if err != nil {
		return Base{}, err
	}
	data.Notebooks = notebooks
	for i := range notebooks {
		if notebooks[i].IsDefault {
			data.DefaultNotebook = &notebooks[i]
		}
	}
	return data, nil
}
