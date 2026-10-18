// Command server runs the user-service HTTP API.
package main

import (
	"fmt"
	"os"

	"github.com/piresc/go-project/internal/user/app"
)

func main() {
	a, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "user-service: init:", err)
		os.Exit(1)
	}
	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "user-service: run:", err)
		os.Exit(1)
	}
}
