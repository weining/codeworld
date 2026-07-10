package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type uiPalette struct {
	accent    string
	secondary string
	text      string
	muted     string
	border    string
	success   string
	warning   string
	error     string
	user      string
}

func paletteFor(theme string) uiPalette {
	switch theme {
	case "light":
		return uiPalette{accent: "25", secondary: "61", text: "235", muted: "242", border: "250", success: "28", warning: "166", error: "160", user: "31"}
	case "dark":
		return uiPalette{accent: "81", secondary: "141", text: "252", muted: "245", border: "238", success: "78", warning: "214", error: "203", user: "117"}
	default:
		return uiPalette{accent: "69", secondary: "99", text: "252", muted: "245", border: "240", success: "42", warning: "214", error: "196", user: "75"}
	}
}

func styled(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

func joinEdges(left, right string, width int) string {
	if width <= 0 {
		return left + "  " + right
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return lipgloss.NewStyle().MaxWidth(width).Render(left)
	}
	return left + strings.Repeat(" ", gap) + right
}

func compactCount(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}
