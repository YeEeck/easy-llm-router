package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type helpBinding struct {
	key         string
	description string
}

func renderHelp(width int, bindings ...helpBinding) string {
	if width <= 0 {
		width = 80
	}
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	descriptionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	var lines []string
	var line string
	for _, binding := range bindings {
		group := keyStyle.Render(binding.key) + " " + descriptionStyle.Render(binding.description)
		candidate := group
		if line != "" {
			candidate = line + "  " + group
		}
		if line != "" && lipgloss.Width(candidate) > width {
			lines = append(lines, line)
			line = group
			continue
		}
		line = candidate
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func contentWidth(windowWidth int) int {
	if windowWidth <= 0 {
		return 80
	}
	return max(1, windowWidth-4)
}

func inputWidth(windowWidth, reserved int) int {
	if windowWidth <= 0 {
		return 48
	}
	return max(1, windowWidth-reserved)
}
