package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/flant/addon-operator/pkg/helm/post_renderer"
)

func main() {
	inputBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "couldn't read input from stdin: %s", err)
		os.Exit(1)
	}
	buf := bytes.NewBuffer(inputBytes)

	renderer := post_renderer.NewPostRenderer(map[string]string{
		"heritage": "addon-operator",
	})

	outputBytes, err := renderer.Run(buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not render input from stdin: %s", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(outputBytes.Bytes()); err != nil {
		fmt.Fprintf(os.Stderr, "could not write rendered output to stdout: %s", err)
		os.Exit(1)
	}
}
