package api

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"abhiudayg.com/github/gitopsctl/internal/api/sessions"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
)

type Scope int

const (
	ScopeRead Scope = iota + 1
	ScopeSync
	ScopeAdmin
)

var scopeNames = map[string]Scope{
	"read":  ScopeRead,
	"sync":  ScopeSync,
	"admin": ScopeAdmin,
}

func ParseScope(name string) (Scope, error) {
	scope, ok := scopeNames[strings.ToLower(name)]
	if !ok {
		return 0, fmt.Errorf("unknown scope %q (want read, sync or admin)", name)
	}
	return scope, nil
}

const agentKeyword = "agent"

func ParseTokens(r io.Reader) (map[string]Scope, error) {
	tokens, _, err := parseTokenLines(r)
	return tokens, err
}

func ParseAgentTokens(r io.Reader) (map[string]string, error) {
	_, agents, err := parseTokenLines(r)
	return agents, err
}

func parseTokenLines(r io.Reader) (map[string]Scope, map[string]string, error) {
	tokens := map[string]Scope{}
	agents := map[string]string{}
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == agentKeyword {
			if len(fields) != 3 {
				return nil, nil, fmt.Errorf("line %d: want \"agent <token> <agent-id>\"", lineNo)
			}
			agents[fields[1]] = fields[2]
			continue
		}
		if len(fields) != 2 {
			return nil, nil, fmt.Errorf("line %d: want \"<scope> <token>\"", lineNo)
		}
		scope, err := ParseScope(fields[0])
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		tokens[fields[1]] = scope
	}
	for token := range agents {
		if _, clash := tokens[token]; clash {
			return nil, nil, fmt.Errorf("a token is used for both an agent and a scope")
		}
	}
	return tokens, agents, scanner.Err()
}

func isSessionPath(path string) bool {
	return path == "/api/v1/sessions" || strings.HasPrefix(path, "/api/v1/sessions/")
}

func requiredScope(method, path string) Scope {
	switch {
	case method == http.MethodGet || method == http.MethodHead:
		return ScopeRead
	case method == http.MethodPost && (strings.HasSuffix(path, "/sync") || strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/check") || strings.HasSuffix(path, "/plan")):
		return ScopeSync
	default:
		return ScopeAdmin
	}
}

func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}

func authMiddleware(tokens map[string]Scope, agents map[string]string, audit func(events.Type, map[string]any)) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			presented, found := strings.CutPrefix(req.Header.Get(echo.HeaderAuthorization), "Bearer ")

			var scope Scope
			var matched, agentID string
			if found {
				for token, tokenScope := range tokens {
					if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1 {
						scope, matched = tokenScope, token
					}
				}
				for token, id := range agents {
					if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1 {
						scope, matched, agentID = 0, token, id
					}
				}
			}
			if matched == "" {
				c.Response().Header().Set(echo.HeaderWWWAuthenticate, "Bearer")
				return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
			}
			onSessions := isSessionPath(req.URL.Path)
			switch {
			case agentID != "" && !onSessions,
				agentID == "" && onSessions && scope < ScopeAdmin,
				agentID == "" && !onSessions && scope < requiredScope(req.Method, req.URL.Path):
				return echo.NewHTTPError(http.StatusForbidden, "insufficient scope")
			}
			c.Set(sessions.AgentKey, agentID)

			err := next(c)
			status := c.Response().Status
			if httpErr := (*echo.HTTPError)(nil); errors.As(err, &httpErr) {
				status = httpErr.Code
			}
			if req.Method != http.MethodGet && req.Method != http.MethodHead && audit != nil {
				fields := map[string]any{
					"token_id": tokenID(matched),
					"method":   req.Method,
					"path":     req.URL.Path,
					"status":   status,
				}
				if agentID != "" {
					fields["agent"] = agentID
				}
				audit(events.TypeAPIAudit, fields)
			}
			return err
		}
	}
}
