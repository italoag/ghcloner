package main

import (
	"context"
	"fmt"
	"os"

	"github.com/italoag/repocloner/internal/interfaces/cli/fang"
)

func main() {
	// Execute the Fang-integrated CLI
	ctx := context.Background()
	if err := fang.Execute(ctx); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
