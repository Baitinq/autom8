package cmd

import (
	"github.com/charmbracelet/lipgloss"
)

// Styles for terminal output
var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	SuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	StatusPendingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true)

	StatusInProgressStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("33")).
				Bold(true)

	StatusReadyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("99")).
				Bold(true)

	StatusCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("42")).
				Bold(true)

	IDStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	NameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("36")).
			Bold(true)

	HighlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("99"))
)
