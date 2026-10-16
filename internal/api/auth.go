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

	"aeswibon.com/github/gitopsctl/internal/events"
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

func ParseTokens(r io.Reader) (map[string]Scope, error) {
	tokens := map[string]Scope{}
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: want \"<scope> <token>\"", lineNo)
		}
		scope, err := ParseScope(fields[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		tokens[fields[1]] = scope
	}
	return tokens, scanner.Err()
}

func requiredScope(method, path string) Scope {
	switch {
	case method == http.MethodGet || method == http.MethodHead:
		return ScopeRead
	case method == http.MethodPost && (strings.HasSuffix(path, "/sync") || strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/check")):
		return ScopeSync
	default:
		return ScopeAdmin
	}
}

func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}

func authMiddleware(tokens map[string]Scope, audit func(events.Type, map[string]any)) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			presented, found := strings.CutPrefix(req.Header.Get(echo.HeaderAuthorization), "Bearer ")

			var scope Scope
			var matched string
			if found {
				for token, tokenScope := range tokens {
					if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1 {
						scope, matched = tokenScope, token
					}
				}
			}
			if matched == "" {
				c.Response().Header().Set(echo.HeaderWWWAuthenticate, "Bearer")
				return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
			}
			if scope < requiredScope(req.Method, req.URL.Path) {
				return echo.NewHTTPError(http.StatusForbidden, "insufficient scope")
			}

			err := next(c)
			status := c.Response().Status
			if httpErr := (*echo.HTTPError)(nil); errors.As(err, &httpErr) {
				status = httpErr.Code
			}
			if req.Method != http.MethodGet && req.Method != http.MethodHead && audit != nil {
				audit(events.TypeAPIAudit, map[string]any{
					"token_id": tokenID(matched),
					"method":   req.Method,
					"path":     req.URL.Path,
					"status":   status,
				})
			}
			return err
		}
	}
}
