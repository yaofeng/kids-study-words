package web

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/store"
)

const (
	sessionCookieName = "sw_session"
	flashCookieName   = "sw_flash"
	sessionTTL        = 30 * 24 * time.Hour
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
)

func currentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(ctxUser).(store.User)
	return u
}

func currentSession(r *http.Request) *store.Session {
	s, _ := r.Context().Value(ctxSession).(*store.Session)
	return s
}

func csrfFrom(r *http.Request) string {
	if s := currentSession(r); s != nil {
		return s.CSRFToken
	}
	return ""
}

// publicPath lists routes that do not require an account or a session.
func publicPath(path string) bool {
	switch path {
	case "/healthz", "/login", "/setup":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

func isHtmx(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// recoverMiddleware turns panics into 500s instead of killing the process.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", v)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'"

// securityHeaders sets the browser-hardening headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// loggingMiddleware logs one line per request without sensitive fields.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", status, "dur", time.Since(start).String())
	})
}

// sessionMiddleware resolves the cookie session and enforces login.
func (s *Server) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := s.now().UTC()
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			hash := auth.HashToken(c.Value)
			sess, err := s.store.SessionByToken(r.Context(), hash)
			if err == nil && sess.ExpiresAt.After(now) {
				user, uerr := s.store.UserBySession(r.Context(), hash, now)
				if uerr == nil {
					ctx := context.WithValue(r.Context(), ctxUser, user)
					ctx = context.WithValue(ctx, ctxSession, &sess)
					r = r.WithContext(ctx)
				}
			}
		}

		if currentSession(r) == nil && !publicPath(r.URL.Path) {
			if isHtmx(r) {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// setupGateMiddleware sends first-run visitors to /setup.
func (s *Server) setupGateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		n, err := s.store.CountUsers(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if n == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// csrfMiddleware rejects state-changing requests without a valid token.
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		if sess := currentSession(r); sess != nil {
			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
					// A plain form cannot set headers; multipart bodies are not
					// covered by ParseForm, so parse them here with a hard cap.
					r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
					if err := r.ParseMultipartForm(maxImportBytes); err != nil {
						http.Error(w, "上传内容太大或格式不对。", http.StatusRequestEntityTooLarge)
						return
					}
				} else {
					_ = r.ParseForm()
				}
				token = r.PostForm.Get("csrf_token")
			}
			if !auth.SecureEqual(token, sess.CSRFToken) {
				http.Error(w, "无效的请求令牌", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Setup and login happen before a session exists; require a
		// same-origin browser request instead.
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.Host != r.Host {
			http.Error(w, "无效的请求来源", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the remote IP for rate limiting.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
