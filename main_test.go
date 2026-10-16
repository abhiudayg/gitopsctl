package main

import (
	"testing"

	"abhiudayg.com/github/gitopsctl/cmd"
)

func TestRootCommand_IsInitialized(t *testing.T) {
	if cmd.RootCmd() == nil {
		t.Fatal("expected root command to be initialized")
	}
}
