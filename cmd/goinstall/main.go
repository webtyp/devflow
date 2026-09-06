package main

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"

	"webtyp.com/devflow"
)

func main() {
	git, err := gitmod.NewGit()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	goHandler, err := devflow.NewGo(git)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	// standalone install defaults to "dev" or current version
	if err := goHandler.Install(""); err != nil {
		fmt.Println("Install failed:", err)
		os.Exit(1)
	}
}
