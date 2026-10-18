// Command server runs the asset-service HTTP API.
package main

import (
	"fmt"
	"os"

	"github.com/piresc/go-project/services/asset-service/internal/app"
)

func main() {
	a, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "asset-service: init:", err)
		os.Exit(1)
	}
	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "asset-service: run:", err)
		os.Exit(1)
	}
}
