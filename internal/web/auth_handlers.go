package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/store"
)

const defaultNotebookName = "默认生词本"

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if n > 0 {
		s.renderError(w, r, http.StatusNotFound, "页面不存在，检查一下网址吧。")
		return
	}
	data, ok := s.pageData(w, r, "首次设置", "")
	if !ok {
		return
	}
	_ = s.renderer.page(w, http.StatusOK, "setup", data)
}

func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	username := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")
	confirm := r.PostForm.Get("confirm")

	renderForm := func(msg string) {
		data, ok := s.pageData(w, r, "首次设置", "")
		if !ok {
			return
		}
		data.Username = username
		data.Error = msg
		_ = s.renderer.page(w, http.StatusOK, "setup", data)
	}

	switch {
	case !auth.ValidUsername(username):
		renderForm("用户名需为 1–32 个字符，不能为空。")
		return
	case !auth.ValidPassword(password):
		renderForm("密码至少 6 位，最多 72 字节。")
		return
	case password != confirm:
		renderForm("两次输入的密码不一致。")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	user, _, err := s.store.CreateFirstUser(r.Context(), username, hash, defaultNotebookName)
	if errors.Is(err, store.ErrSetupComplete) {
		s.renderError(w, r, http.StatusNotFound, "页面不存在，检查一下网址吧。")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	if _, err := s.startSession(w, r, user.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/lookup", http.StatusSeeOther)
}

// handleRegisterPage shows the sign-up form. Registration needs an existing
// account to vouch for the new one.
func (s *Server) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if n == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if currentUser(r).ID != 0 {
		http.Redirect(w, r, "/lookup", http.StatusSeeOther)
		return
	}
	data, ok := s.pageData(w, r, "注册新用户", "")
	if !ok {
		return
	}
	_ = s.renderer.page(w, http.StatusOK, "register", data)
}

// handleRegisterPost creates an account after checking the credentials of an
// existing account.
func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if n == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	ip := clientIP(r)
	if !s.limiter.Allow(ip) {
		data, ok := s.pageData(w, r, "注册新用户", "")
		if !ok {
			return
		}
		data.Error = "试了太多次，请稍后再试。"
		_ = s.renderer.page(w, http.StatusTooManyRequests, "register", data)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	authorizer := strings.TrimSpace(r.PostForm.Get("authorizer"))
	authorizerPassword := r.PostForm.Get("authorizer_password")
	username := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")
	confirm := r.PostForm.Get("confirm")

	renderForm := func(msg string) {
		data, ok := s.pageData(w, r, "注册新用户", "")
		if !ok {
			return
		}
		data.Authorizer = authorizer
		data.Username = username
		data.Error = msg
		_ = s.renderer.page(w, http.StatusOK, "register", data)
	}

	user, err := s.store.UserByUsername(r.Context(), authorizer)
	if err != nil || !auth.CheckPassword(user.PasswordHash, authorizerPassword) {
		s.limiter.Fail(ip)
		renderForm("现有账号的用户名或密码不对。")
		return
	}
	s.limiter.Reset(ip)

	switch {
	case !auth.ValidUsername(username):
		renderForm("新用户名需为 1–32 个字符，不能为空。")
		return
	case !auth.ValidPassword(password):
		renderForm("密码至少 6 位，最多 72 字节。")
		return
	case password != confirm:
		renderForm("两次输入的密码不一致。")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, _, err := s.store.CreateUser(r.Context(), username, hash, defaultNotebookName); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			renderForm("这个用户名已经有人用了，换一个吧。")
			return
		}
		s.fail(w, r, err)
		return
	}

	setFlash(w, "新账号已创建，用新用户名登录吧。")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if n == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if currentUser(r).ID != 0 {
		http.Redirect(w, r, "/lookup", http.StatusSeeOther)
		return
	}
	data, ok := s.pageData(w, r, "登录", "")
	if !ok {
		return
	}
	_ = s.renderer.page(w, http.StatusOK, "login", data)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.Allow(ip) {
		data, ok := s.pageData(w, r, "登录", "")
		if !ok {
			return
		}
		data.Error = "试了太多次，请稍后再试。"
		_ = s.renderer.page(w, http.StatusTooManyRequests, "login", data)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	username := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")

	renderLoginError := func(msg string) {
		data, ok := s.pageData(w, r, "登录", "")
		if !ok {
			return
		}
		data.Username = username
		data.Error = msg
		_ = s.renderer.page(w, http.StatusOK, "login", data)
	}

	user, err := s.store.UserByUsername(r.Context(), username)
	if err != nil || !auth.CheckPassword(user.PasswordHash, password) {
		s.limiter.Fail(ip)
		renderLoginError("用户名或密码不对。")
		return
	}
	s.limiter.Reset(ip)

	if _, err := s.startSession(w, r, user.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/lookup", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// startSession creates a database session and sets the cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) (*store.Session, error) {
	raw, hash, err := auth.NewToken()
	if err != nil {
		return nil, err
	}
	csrf, err := auth.NewCSRFToken()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	expires := now.Add(sessionTTL)
	if err := s.store.CreateSession(r.Context(), hash, userID, csrf, now, expires); err != nil {
		return nil, err
	}
	_ = s.store.DeleteExpiredSessions(r.Context(), now)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return &store.Session{TokenHash: hash, UserID: userID, CSRFToken: csrf, CreatedAt: now, ExpiresAt: expires}, nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}
