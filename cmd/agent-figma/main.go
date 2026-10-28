package main

import "github.com/shhac/agent-figma/internal/cli"

var version = "dev"

func main() {
	cli.Execute(version)
}
