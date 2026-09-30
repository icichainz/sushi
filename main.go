package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/app"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fonts"
	"github.com/icichainz/sushi/internal/shell"
	"github.com/icichainz/sushi/internal/ui"
)

// optionalString is a flag that can be given alone or with a value, as in
// --print-shell-wrapper or --print-shell-wrapper=zsh
type optionalString struct {
	set   bool
	value string
}

func (o *optionalString) String() string   { return o.value }
func (o *optionalString) IsBoolFlag() bool { return true }
func (o *optionalString) Set(s string) error {
	o.set = true
	if s != "true" {
		o.value = s
	}
	return nil
}

func main() {
	// Parse command line flags
	asciiMode := flag.Bool("ascii", false, "Use ASCII icons (no icon font required)")
	bootstrapMode := flag.Bool("bootstrap", false, "Use Bootstrap Icons font")
	installFont := flag.Bool("install-font", false, "Download and install a Nerd Font (JetBrainsMono unless --font is given)")
	fontName := flag.String("font", "", "Nerd Font to install with --install-font (see --list-fonts)")
	listFonts := flag.Bool("list-fonts", false, "List available Nerd Fonts to install")
	initConfig := flag.Bool("init-config", false, "Create default configuration file")
	cwdFile := flag.String("cwd-file", "", "On quit, write the directory shown to this `file` (Q quits without writing)")
	var wrapperShell optionalString
	flag.Var(&wrapperShell, "print-shell-wrapper", "Print the sushicd shell function for zsh, bash or fish (default: $SHELL)")
	showHelp := flag.Bool("help", false, "Show help message")
	flag.BoolVar(showHelp, "h", false, "Show help message")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Sushi - A fast and elegant terminal file explorer\n\n")
		fmt.Fprintf(os.Stderr, "Usage: sushi [options] [directory]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nIcon Modes:\n")
		fmt.Fprintf(os.Stderr, "  Default: Nerd Font icons (requires Nerd Font installed)\n")
		fmt.Fprintf(os.Stderr, "  --bootstrap: Bootstrap Icons (requires Bootstrap Icons font)\n")
		fmt.Fprintf(os.Stderr, "  --ascii: ASCII text icons (works everywhere)\n")
		fmt.Fprintf(os.Stderr, "\nFont Installation:\n")
		fmt.Fprintf(os.Stderr, "  --install-font: Download and install JetBrainsMono Nerd Font\n")
		fmt.Fprintf(os.Stderr, "  --install-font --font FiraCode: Install a different font from --list-fonts\n")
		fmt.Fprintf(os.Stderr, "  --list-fonts: Show available Nerd Fonts\n")
		fmt.Fprintf(os.Stderr, "\nConfiguration:\n")
		fmt.Fprintf(os.Stderr, "  --init-config: Create default config file at ~/.config/sushi/config.yaml\n")
		fmt.Fprintf(os.Stderr, "  Config file settings: icon_mode, preview_enabled, preview_width, etc.\n")
		fmt.Fprintf(os.Stderr, "\nShell Integration:\n")
		fmt.Fprintf(os.Stderr, "  A program can't change its shell's directory, so sushicd runs sushi and cds\n")
		fmt.Fprintf(os.Stderr, "  to the directory it was showing when you quit with q (Q quits without).\n")
		fmt.Fprintf(os.Stderr, "  eval \"$(sushi --print-shell-wrapper zsh)\"   # in ~/.zshrc (bash: ~/.bashrc)\n")
		fmt.Fprintf(os.Stderr, "  sushi --print-shell-wrapper fish | source    # in ~/.config/fish/config.fish\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  sushi              # Open current directory (Nerd Font icons)\n")
		fmt.Fprintf(os.Stderr, "  sushi ~/projects   # Open specific directory\n")
		fmt.Fprintf(os.Stderr, "  sushi --install-font  # Install Nerd Font for icons\n")
		fmt.Fprintf(os.Stderr, "  sushi --ascii      # Use ASCII icons\n")
		fmt.Fprintf(os.Stderr, "  sushi --init-config   # Create configuration file\n")
	}

	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// Print the shell function, for the shell named after the flag or in
	// $SHELL
	if wrapperShell.set {
		name := wrapperShell.value
		if name == "" && flag.NArg() > 0 {
			name = flag.Arg(0)
		}
		if name == "" {
			name = os.Getenv("SHELL")
		}
		script, err := shell.Wrapper(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(script)
		os.Exit(0)
	}

	// Handle font listing
	if *listFonts {
		fmt.Println("Available Nerd Fonts to install:")
		fmt.Println()
		for _, font := range fonts.AvailableFonts {
			fmt.Printf("  - %s (v%s)\n", font.Name, font.Version)
		}
		fmt.Println()
		fmt.Println("Install with: sushi --install-font [--font NAME]")
		fmt.Println()

		// Check for installed fonts
		installed, err := fonts.ListInstalledNerdFonts()
		if err == nil && len(installed) > 0 {
			fmt.Println("Already installed:")
			for _, name := range installed {
				fmt.Printf("  - %s\n", name)
			}
		}
		os.Exit(0)
	}

	// Handle font installation
	if *installFont {
		font := fonts.GetDefaultFont()
		if *fontName != "" {
			found, ok := fonts.FindFont(*fontName)
			if !ok {
				fmt.Printf("Unknown font %q. Run 'sushi --list-fonts' to see the choices.\n", *fontName)
				os.Exit(1)
			}
			font = found
		}
		fmt.Printf("Installing %s Nerd Font...\n\n", font.Name)

		err := fonts.InstallFont(font, func(status string) {
			fmt.Println(status)
		})

		if err != nil {
			fmt.Printf("\nError: %v\n", err)
			os.Exit(1)
		}

		fmt.Println()
		fmt.Println("Installation complete!")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  1. Open your terminal preferences/settings")
		fmt.Printf("  2. Change the font to '%s Nerd Font'\n", font.Name)
		fmt.Println("  3. Restart your terminal")
		fmt.Println("  4. Run 'sushi' to enjoy file icons!")
		os.Exit(0)
	}

	// Handle config initialization
	if *initConfig {
		created, err := config.CreateDefaultConfigFile()
		if err != nil {
			fmt.Printf("Error creating config file: %v\n", err)
			os.Exit(1)
		}

		configPath := config.GetConfigPath()
		if created {
			fmt.Printf("Created default configuration file at:\n  %s\n\n", configPath)
			fmt.Println("You can edit this file to customize sushi settings.")
		} else {
			fmt.Printf("Configuration file already exists at:\n  %s\n", configPath)
		}
		os.Exit(0)
	}

	// Load configuration
	cfg := config.LoadConfig()

	// Set icon mode: CLI flags take precedence over config
	if *asciiMode {
		ui.SetIconMode(ui.IconModeASCII)
		cfg.IconMode = "ascii"
	} else if *bootstrapMode {
		ui.SetIconMode(ui.IconModeBootstrap)
		cfg.IconMode = "bootstrap"
	} else {
		// Apply icon mode from config
		switch cfg.IconMode {
		case "ascii":
			ui.SetIconMode(ui.IconModeASCII)
		case "bootstrap":
			ui.SetIconMode(ui.IconModeBootstrap)
		default:
			ui.SetIconMode(ui.IconModeNerd)
		}
	}

	// Get starting directory (current dir or from remaining args)
	startPath, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	// Use remaining argument as path if provided
	if flag.NArg() > 0 {
		startPath = flag.Arg(0)
	}

	// Create the initial model with config
	m := app.NewModelWithConfig(startPath, cfg)

	// Run the program
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if model, ok := final.(app.Model); ok {
		model.Close()
		// For the shell function to cd to; see --print-shell-wrapper
		if err == nil && *cwdFile != "" {
			if err := model.WriteExitDir(*cwdFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
	}
	if err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
