// Command price-service runs the price-service HTTP API.
package main

import (
	"fmt"
	"os"

	"github.com/piresc/go-project/internal/price/app"
)

func main() {
	a, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "price-service: init:", err)
		os.Exit(1)
	}
	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "price-service: run:", err)
		os.Exit(1)
	}
}
