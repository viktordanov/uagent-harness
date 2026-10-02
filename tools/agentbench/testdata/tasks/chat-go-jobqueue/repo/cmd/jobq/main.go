// Command jobq is a job queue on the command line: add jobs to a JSON file,
// run them with a pool of workers, and look at what happened.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
