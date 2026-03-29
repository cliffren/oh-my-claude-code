package main

import (
	"github.com/cliffren/oh-my-claude-code/cmd"
	"github.com/cliffren/oh-my-claude-code/internal/logging"
)

func main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
