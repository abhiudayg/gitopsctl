package session

import (
	"context"
	"encoding/json"
	"os"

	"abhiudayg.com/github/gitopsctl/internal/core/git"
	"go.uber.org/zap"
)

func jsonMarshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func gitFetcher(logger *zap.Logger) Fetcher {
	return func(ctx context.Context, repo, ref string) (string, string, func(), error) {
		dir, err := git.CreateTempRepoDir()
		if err != nil {
			return "", "", nil, err
		}
		cleanup := func() { _ = os.RemoveAll(dir) }
		commit, err := git.CloneOrPull(ctx, logger, repo, ref, dir)
		if err != nil {
			cleanup()
			return "", "", nil, err
		}
		return dir, commit, cleanup, nil
	}
}
