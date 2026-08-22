// Command freetube-sync syncs FreeTube subscriptions across devices,
// acting as both server and client depending on the subcommand invoked.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via:
//
//	go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "-version", "--version":
		fmt.Println("freetube-sync " + version)
		return
	case "-h", "--help", "help":
		usage(os.Stdout)
		return
	case "serve":
		err = cmdServe(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "sync":
		err = cmdSync(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "inspect":
		err = cmdInspect(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "freetube-sync: unknown subcommand %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "freetube-sync %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `freetube-sync — sync FreeTube subscriptions across devices

Usage:
  freetube-sync serve    --listen :8080 --data /var/lib/freetube-sync --token <secret>
  freetube-sync run      --server https://host:8080 --token <secret> [--install=flatpak|native]
  freetube-sync sync     --server https://host:8080 --token <secret> [--install=flatpak|native]
  freetube-sync status   [--install=flatpak|native]
  freetube-sync inspect  --db path/to/profiles.db
  freetube-sync --version
`)
}
