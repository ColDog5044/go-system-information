package main

import (
	"fmt"
	"os"
)

func main() {
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error retrieving hostname:", err)
		return
	}

	fmt.Println("System Information:")
	fmt.Println("")
	fmt.Println("Hostname:", hostname)
}

