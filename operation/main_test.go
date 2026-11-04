package operation_test

import (
	"os"
	"testing"

	"gitea.com/gitea/gitea-mcp/cmd"
)

func TestMain(m *testing.M) {
	if os.Getenv("GITEA_MCP_TEST_SERVE") == "1" {
		cmd.Execute()
		return
	}
	m.Run()
}
