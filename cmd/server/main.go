// Command server runs the 少儿背单词 web application.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/config"
	"github.com/yaofeng/kids-study-words/internal/store"
	"github.com/yaofeng/kids-study-words/internal/web"
	"golang.org/x/term"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Println(version)
			return nil
		case "healthcheck":
			return healthcheck()
		case "reset-password":
			return resetPassword(args[1:])
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}
	return serve()
}

func healthcheck() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + cfg.Port + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func resetPassword(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: study-words reset-password <username>")
	}
	username := strings.TrimSpace(args[0])
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "study.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	user, err := st.UserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user %q: %w", username, err)
	}

	pw, err := readPassword("新密码（至少 6 位）: ")
	if err != nil {
		return err
	}
	if !auth.ValidPassword(pw) {
		return errors.New("密码长度需为 6–72 字节")
	}
	confirm, err := readPassword("再次输入新密码: ")
	if err != nil {
		return err
	}
	if pw != confirm {
		return errors.New("两次输入的密码不一致")
	}

	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	if err := st.UpdatePassword(ctx, user.ID, hash); err != nil {
		return err
	}
	if err := st.DeleteSessionsForUser(ctx, user.ID); err != nil {
		return err
	}
	fmt.Println("密码已更新，所有已登录会话已失效。")
	return nil
}

// readPassword reads one line without echo when stdin is a terminal.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	fmt.Fprintln(os.Stderr)
	return strings.TrimRight(line, "\r\n"), nil
}

func serve() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", web.HealthHandler)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
