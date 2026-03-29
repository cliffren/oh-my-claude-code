package main

import (
	"github.com/cliffren/toc/cmd"
	"github.com/cliffren/toc/internal/logging"
)

func main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
