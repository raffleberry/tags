package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/raffleberry/tags"
)

func main() {
	args := os.Args
	if args[0] == "go" && len(args) > 2 && args[1] == "run" {
		args = args[2:]
	}

	if len(args) < 2 {
		fmt.Println("USAGE: ", args[0], "<audio file>")
		os.Exit(1)
	}

	t, err := tags.Read(args[1])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer t.Close()

	j, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(j))
}
