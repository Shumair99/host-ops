package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/Shumair99/host-ops/internal/console"
	"github.com/charmbracelet/x/ansi"
)

// Only renderer-owned strings contain escapes. Evidence enters through clean.
type theme struct{ color bool }

const (
	ink    = 252
	muted  = 247
	edge   = 240
	cyan   = 180 // warm brass: navigation / CPU
	mint   = 108 // sage: passing evidence
	violet = 110 // slate blue: memory
	amber  = 179
	red    = 174
)

func clean(s string) string {
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(console.Safe(s))
}
func (t theme) paint(s string, color int) string {
	if !t.color || s == "" {
		return s
	}
	return fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0m", color, s)
}
func (t theme) plot(s string, color int) string {
	if !t.color {
		return ansi.Strip(strings.ReplaceAll(s, "\x1b[4m \x1b[24m", "─"))
	}
	// Background-filled cells cover the entire terminal cell, independently
	// of the font's full-block bearings. Half cells retain their real height.
	s = strings.ReplaceAll(s, "█", t.solid(1, color))
	return t.paint(s, color)
}
func (t theme) solid(width, color int) string {
	if width <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[48;5;%dm%s\x1b[49m", color, strings.Repeat(" ", width))
}
func (t theme) strong(s string, color int) string {
	if !t.color {
		return s
	}
	return fmt.Sprintf("\x1b[1;38;5;%dm%s\x1b[0m", color, s)
}
func (t theme) selection(s string, width int) string {
	s = pad(s, max(0, width-2)) + " ‹"
	s = clip(s, width)
	if !t.color {
		return ansi.Strip(s)
	}
	s = strings.ReplaceAll(s, "\x1b[49m", "\x1b[48;5;236m")
	return "\x1b[38;5;252;48;5;236m" + strings.ReplaceAll(s, "\x1b[0m", "\x1b[0;38;5;252;48;5;236m") + "\x1b[0m"
}
func clip(s string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}
func pad(s string, width int) string {
	s = clip(strings.NewReplacer("\n", " ", "\t", " ").Replace(s), width)
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}
func wrap(lines []string, width int) []string {
	if width < 1 {
		return nil
	}
	out := []string{}
	for _, line := range lines {
		out = append(out, strings.Split(ansi.Wrap(line, width, ""), "\n")...)
	}
	return out
}
func (t theme) panel(title string, rows []string, width int) []string {
	inside := max(1, width-4)
	label := " " + clip(title, max(1, width-5)) + " "
	top := t.paint("┌─", edge) + t.strong(label, cyan) + t.paint(strings.Repeat("─", max(0, width-3-ansi.StringWidth(label)))+"┐", edge)
	out := []string{top}
	for _, row := range rows {
		for _, line := range wrap([]string{row}, inside) {
			out = append(out, t.paint("│", edge)+" "+pad(line, inside)+" "+t.paint("│", edge))
		}
	}
	return append(out, t.paint("└"+strings.Repeat("─", max(0, width-2))+"┘", edge))
}
func columns(left, right []string, leftWidth, rightWidth int) []string {
	out := []string{}
	for i := 0; i < max(len(left), len(right)); i++ {
		a, b := "", ""
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		out = append(out, pad(a, leftWidth)+" "+pad(b, rightWidth))
	}
	return out
}
func value(values map[string]float64, key string) *float64 {
	n, ok := values[key]
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
func percent(n *float64) string {
	if n == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", *n)
}
func metricColor(n *float64, normal int) int {
	if n == nil {
		return muted
	}
	if *n >= 95 {
		return red
	}
	if *n >= 80 {
		return amber
	}
	return normal
}
func (t theme) meter(n *float64, width, color int) string {
	width = max(1, width)
	if n == nil {
		if t.color {
			return t.solid(width, 235)
		}
		return t.paint(strings.Repeat("░", width), edge)
	}
	fill := int(math.Round(math.Max(0, math.Min(100, *n)) * float64(width) / 100))
	if t.color {
		return t.solid(fill, color) + t.solid(width-fill, 235)
	}
	return t.paint(strings.Repeat("█", fill), color) + t.paint(strings.Repeat("░", width-fill), edge)
}
func statusColor(s string) int {
	switch strings.ToLower(s) {
	case "healthy", "pass", "running", "active", "completed", "resolved", "ok", "connected", "ready", "succeeded", "verified":
		return mint
	case "warning", "warn", "provisional", "connecting":
		return amber
	case "critical", "fail", "failed", "open", "exited":
		return red
	default:
		return muted
	}
}
func (t theme) badge(s string) string { return t.strong(strings.ToUpper(clean(s)), statusColor(s)) }
