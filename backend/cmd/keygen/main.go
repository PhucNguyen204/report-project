package main

import (
	"dicho/internal/app"
	"flag"
	"fmt"
	"os"
)

func main() {
	dir := flag.String("out", ".local", "directory for local signing and queue keys")
	flag.Parse()
	if err := app.WriteLocalKeys(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Private keys ready in", *dir)
}
