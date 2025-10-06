package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/app"
)

func main() {
	// Get starting directory (current dir or from args)
	startPath, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		startPath = os.Args[1]
	}

	// Create the initial model
	m := app.NewModel(startPath)

	// Run the program
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
