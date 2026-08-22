package main

import (
	"flag"
	"fmt"
	"os"

	"freetube-sync/internal/nedb"
)

func cmdInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to profiles.db (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		fs.Usage()
		os.Exit(2)
	}

	db, err := nedb.ReadFile(*dbPath)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %d document(s)\n", *dbPath, len(db.Docs))

	subs, err := db.GetSubscriptions()
	if err != nil {
		return fmt.Errorf("read subscriptions: %w", err)
	}

	fmt.Printf("All Channels: %d subscription(s)\n", len(subs))
	for _, s := range subs {
		fmt.Printf("  %-26s %s\n", s.ID, s.Name)
	}
	return nil
}
