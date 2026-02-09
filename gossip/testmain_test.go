package gossip

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	identityDir, err := os.MkdirTemp("", "vcs-gossip-tests-identity-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv(identityDirEnvVar, filepath.Join(identityDir, "vcs", "gossip"))

	code := m.Run()
	_ = os.RemoveAll(identityDir)
	os.Exit(code)
}
