package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/dict"
	"github.com/yaofeng/kids-study-words/internal/store"
)

// Dictionary is the subset of the word dictionary the MCP tools need.
type Dictionary interface {
	Lookup(ctx context.Context, word string) (dict.Entry, error)
	Examples(ctx context.Context, word, exchange string, limit int) ([]dict.Sentence, error)
}

// Config supplies the dependencies and environment shared by every tool.
type Config struct {
	Store    *store.Store
	Dict     Dictionary
	Location *time.Location
	Version  string
	Now      func() time.Time
}

// toolError logs an unexpected store or dependency failure and returns a short
// message safe to show the MCP client. Deliberate validation errors keep their
// own specific text and must not use this helper.
func toolError(action string, err error) error {
	slog.Error(action, "err", err)
	return fmt.Errorf("操作失败，请稍后重试")
}

// newServer builds one user's MCP server; every tool closes over userID.
func newServer(cfg Config, userID int64) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name: "study-words", Title: "少儿背单词", Version: cfg.Version,
	}, &mcp.ServerOptions{
		Instructions: "所有操作都作用于当前访问令牌对应的账号。除 query_recall_logs 的日期参数（YYYY-MM-DD，按服务器时区解释）外，所有时间字段均为 UTC（RFC3339）。需要学习分析时，请用 query_recall_logs 获取原始记录后自行计算。",
	})
	registerNotebookTools(s, cfg, userID)
	registerWordTools(s, cfg, userID)
	registerGroupTools(s, cfg, userID)
	registerRecallTools(s, cfg, userID)
	return s
}

// NewHandler returns an HTTP handler that authenticates MCP requests with a
// bearer token and serves one MCP server per account.
func NewHandler(cfg Config) http.Handler {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Location == nil {
		cfg.Location = time.Local
	}

	verifier := func(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		u, err := cfg.Store.UserByMCPToken(ctx, auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			return nil, sdkauth.ErrInvalidToken
		}
		if err != nil {
			return nil, err
		}
		if err := cfg.Store.TouchMCPToken(ctx, u.ID, cfg.Now()); err != nil {
			slog.Warn("touch mcp token", "user_id", u.ID, "err", err)
		}
		return &sdkauth.TokenInfo{
			UserID: strconv.FormatInt(u.ID, 10),
			Extra:  map[string]any{"user_id": u.ID},
		}, nil
	}

	var servers sync.Map
	getServer := func(r *http.Request) *mcp.Server {
		info := sdkauth.TokenInfoFromContext(r.Context())
		if info == nil {
			return nil
		}
		userID, err := strconv.ParseInt(info.UserID, 10, 64)
		if err != nil {
			return nil
		}
		if v, ok := servers.Load(userID); ok {
			return v.(*mcp.Server)
		}
		s := newServer(cfg, userID)
		actual, _ := servers.LoadOrStore(userID, s)
		return actual.(*mcp.Server)
	}

	h := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		JSONResponse:               true,
		DisableLocalhostProtection: true,
	})
	return sdkauth.RequireBearerToken(verifier, &sdkauth.RequireBearerTokenOptions{
		AllowMissingExpiration: true,
	})(h)
}
