package main

import (
	"context"
	"os"

	"github.com/gnolang/gno/contribs/valdoctor/internal/app"
	"github.com/gnolang/gno/tm2/pkg/commands"
)

func main() {
	cmd := app.NewRootCmd(commands.NewDefaultIO())
	cmd.Execute(context.Background(), os.Args[1:])
}
