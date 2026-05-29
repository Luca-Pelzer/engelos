package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:8080", "Daemon address")
	email := flag.String("email", "", "Login email (prompted if missing)")
	password := flag.String("password", "", "Login password (prompted if missing)")
	insecure := flag.Bool("insecure", false, "Skip TLS verification")
	flag.Parse()

	client, err := NewClient(*addr, *insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "engelos-tui: %v\n", err)
		os.Exit(2)
	}

	model := NewModel(client, *email, *password)
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "engelos-tui: %v\n", err)
		os.Exit(1)
	}
}
