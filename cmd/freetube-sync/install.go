package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"freetube-sync/internal/config"
	"freetube-sync/internal/installdetect"
)

// detectInstall runs installdetect.Detect wired up for interactive use:
// cfg.Install (already flag > env > persisted-config resolved) forces the
// choice if set; otherwise, if both a Flatpak and native install are
// found, it prompts on stdin/stdout once and persists the answer to
// config.json.
func detectInstall(cfg config.Config) (installdetect.Result, error) {
	return installdetect.Detect(installdetect.Options{
		Override: installdetect.Kind(cfg.Install),
		Prompt:   promptInstallChoice,
		Persist:  persistInstallChoice,
	})
}

// promptInstallChoice asks the user, on stdin/stdout, to pick between a
// detected Flatpak and native FreeTube install.
func promptInstallChoice(flatpakDB, nativeDB string) (installdetect.Kind, error) {
	fmt.Printf("Both a Flatpak and a native FreeTube install were found:\n")
	fmt.Printf("  [1] flatpak (%s)\n", flatpakDB)
	fmt.Printf("  [2] native  (%s)\n", nativeDB)
	fmt.Print("Choose 1 or 2: ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	switch strings.TrimSpace(line) {
	case "1", "flatpak":
		return installdetect.Flatpak, nil
	case "2", "native":
		return installdetect.Native, nil
	default:
		return "", fmt.Errorf("unrecognized choice %q", strings.TrimSpace(line))
	}
}

// persistInstallChoice saves the user's install choice to config.json so
// future invocations don't need to prompt again.
func persistInstallChoice(kind installdetect.Kind) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Install = string(kind)
	return config.Save(cfg)
}
