package main

import (
	"fmt"
	"os"

	"github.com/yeck/easy-llm-router/internal/app"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "export":
			if err := app.Export(); err != nil {
				fmt.Fprintln(os.Stderr, "easy-llm-router:", err)
				os.Exit(1)
			}
			return
		case "import":
			if err := app.Import(args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "easy-llm-router:", err)
				os.Exit(1)
			}
			return
		}
	}
	app.Main()
}
