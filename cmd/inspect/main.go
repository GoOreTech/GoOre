// Command goore inspect — read-only TUI inspector for saved world
// data. See internal/inspect/loader.go for the data layer and
// internal/inspect/model.go for the Bubble Tea model.
//
// Usage:
//
//	goore inspect <world-dir>
//
// Exits 0 on user quit, 1 on hard error (missing dir, missing
// world.meta, corrupt world.meta).
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"goore/internal/inspect"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: goore inspect <world-dir>\n")
		os.Exit(1)
	}
	dir := os.Args[1]

	wd, err := inspect.LoadWorld(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Print a one-line summary to stderr before the TUI starts,
	// so an operator running headlessly (or in a script) gets
	// visible feedback even if the TUI never renders.
	fmt.Fprintf(os.Stderr, "Loaded %d players, %d chunks from %s\n",
		len(wd.Players), wd.ChunkCount, dir)
	for _, e := range wd.Errors {
		fmt.Fprintf(os.Stderr, "WARN: skipping %s: %v\n", e.Path, e.Err)
	}

	m := inspect.NewModel(wd)
	p := tea.NewProgram(m, tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: TUI exited with error: %v\n", err)
		os.Exit(1)
	}
}
