// Command npmctl is a write-gated CLI for the Nginx Proxy Manager API.
package main

import (
	"os"

	"github.com/nnmduc/npmctl/internal/cli"
)

// version is set at link time: -ldflags "-X main.version=v1.0.0".
var version = "dev"

func main() {
	cli.SetVersion(version)
	os.Exit(cli.Execute())
}
