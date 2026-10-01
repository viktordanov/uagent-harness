// Command inventory prints an inventory file.
//
//	inventory [-sort name|qty] FILE
//
// FILE has one item per line: name,qty,price (price in dollars, e.g. 1.20).
package main

import (
	"os"

	"example.com/inventory/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
