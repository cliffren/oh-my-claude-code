package main

import (
	"github.com/Krontx/oh-my-claude-code/cmd"
	"github.com/Krontx/oh-my-claude-code/internal/logging"
)

func main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
