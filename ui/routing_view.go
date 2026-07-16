package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yfernandes/smc-mixer-tui/backend/pwbackend"
)

var (
	routingTitleStyle  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	routingRootStyle   = lipgloss.NewStyle().Bold(true)
	routingDimStyle    = lipgloss.NewStyle().Foreground(colorDim)
	routingOKStyle     = lipgloss.NewStyle().Foreground(colorGreen)
	routingWarnStyle   = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	routingHeaderStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	routingCursorStyle = lipgloss.NewStyle().Foreground(colorGold).Bold(true)
)

// categoryLabel returns the display heading for a route category, matching
// the order and naming of the button pages ("applications"/"outputs"/"inputs").
func categoryLabel(category string) string {
	switch category {
	case "applications":
		return "Applications"
	case "outputs":
		return "Outputs"
	case "inputs":
		return "Inputs"
	default:
		return "Other"
	}
}

// mismatchEpsilon is the volume delta above which internal and live values are
// flagged as diverged, rather than treated as float/rounding noise.
const mismatchEpsilon = 0.01

// maxNodeNameLen caps how much of a PipeWire node name is shown per step —
// some device node names (e.g. "alsa_output.pci-0000_08_00.6.analog-stereo")
// are long enough to push the volume columns off screen.
const maxNodeNameLen = 28

// renderRouting renders the routing inspector as a tree: one root per stream
// the daemon manages, a shared trunk (raw stream → fader → null sink) for
// steps common to every path, then branches for each path its signal can
// fork into, each with its own sequence of steps.
func (m Model) renderRouting() string {
	var b strings.Builder
	help := "  (Tab/Esc to close)"
	if len(m.retargetTargets()) > 0 {
		help = "  (↑↓ select branch, Enter to retarget its output, Tab/Esc to close)"
	}
	fmt.Fprintf(&b, "%s%s\n\n", routingTitleStyle.Render("Routing Inspector"), routingDimStyle.Render(help))

	if len(m.routing.Routes) == 0 {
		b.WriteString(routingDimStyle.Render("No managed streams."))
		return b.String()
	}

	targets := m.retargetTargets()
	selNode, selBranch := -1, -1
	if n := len(targets); n > 0 {
		t := targets[m.routingCursor%n]
		selNode, selBranch = t.NodeIdx, t.BranchIdx
	}

	lastCategory := ""
	for i, node := range m.routing.Routes {
		if node.Category != lastCategory {
			if lastCategory != "" {
				b.WriteString("\n")
			}
			lastCategory = node.Category
			header := categoryLabel(lastCategory)
			fmt.Fprintf(&b, "%s\n%s\n", routingHeaderStyle.Render(header), routingDimStyle.Render(strings.Repeat("─", len(header))))
		}

		attach := "(unbound)"
		if node.AttachedCh >= 0 {
			// Displayed channel numbers are 1-based (hardware strips are labeled 1–8);
			// AttachedCh itself stays 0-indexed internally.
			attach = fmt.Sprintf("→ ch%d", node.AttachedCh+1)
		}
		fmt.Fprintf(&b, "%s  %s\n", routingRootStyle.Render(node.StreamName), routingDimStyle.Render(attach))

		// Trunk steps are shared by every branch — same "↳" sequence marker as
		// branch steps, just at the root indent (no fork yet).
		for _, step := range node.Trunk {
			fmt.Fprintf(&b, "  ↳ %s\n", renderStep(step))
		}

		for j, branch := range node.Branches {
			branchTee := "├─"
			if j == len(node.Branches)-1 {
				branchTee = "└─"
			}
			marker, label := "  ", branch.Label
			if i == selNode && j == selBranch {
				marker = routingCursorStyle.Render("▸") + " "
				label = routingCursorStyle.Render(label)
			}
			fmt.Fprintf(&b, "%s%s %s\n", marker, branchTee, label)

			branchBar := "│"
			if j == len(node.Branches)-1 {
				branchBar = " "
			}
			// Steps within a branch are a sequence, not a fork — use "↳" rather
			// than "├─"/"└─" so it doesn't read as alternative sibling options.
			for _, step := range branch.Steps {
				fmt.Fprintf(&b, "  %s   ↳ %s\n", branchBar, renderStep(step))
			}
		}
		// Skip the trailing blank line when the next node starts a new category
		// header — that header already opens with its own blank line, and we
		// don't want two.
		if i < len(m.routing.Routes)-1 && m.routing.Routes[i+1].Category == node.Category {
			b.WriteString("\n")
		}
	}

	out := b.String()
	if m.termW > 0 {
		out = lipgloss.NewStyle().Width(m.termW).Render(out)
	}
	return out
}

// renderRetargetPicker renders the destination-sink picker overlaid below
// the tree when routingPickerOpen is true, listing live output sinks
// (m.pickerCandidates()) with a cursor — same visual language as the bind
// panel (bindBarStyle header, bindCursorStyle/bindItemStyle rows).
func (m Model) renderRetargetPicker() string {
	targets := m.retargetTargets()
	label := ""
	if n := len(targets); n > 0 {
		t := targets[m.routingCursor%n]
		node := m.routing.Routes[t.NodeIdx]
		branch := node.Branches[t.BranchIdx]
		label = fmt.Sprintf("%s / %s", node.StreamName, branch.Label)
	}
	header := bindBarStyle.Render(fmt.Sprintf(" Retarget %s   ↑↓ navigate   enter confirm   esc cancel", label))

	candidates := m.pickerCandidates()
	if len(candidates) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header, bindDimStyle.Render(" (no output sinks available)"))
	}

	cursor := m.routingPickerCursor % len(candidates)
	rows := make([]string, 0, len(candidates))
	for i, s := range candidates {
		prefix := " "
		style := bindItemStyle
		if i == cursor {
			prefix = "▶"
			style = bindCursorStyle
		}
		rows = append(rows, style.Render(prefix+" "+s.Name))
	}
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{header}, rows...)...)
}

func renderStep(s pwbackend.RouteStep) string {
	name := truncateName(s.NodeName)
	if name == "" {
		name = "(none)"
	}
	line := fmt.Sprintf("%-14s %-*s", s.Label, maxNodeNameLen, name)
	if !s.HasVolume {
		return line
	}

	internal := routingDimStyle.Render("int=n/a")
	if s.HasInternal {
		internal = fmt.Sprintf("int=%s", formatPct(s.InternalVolume))
	}

	live := routingWarnStyle.Render("live=MISSING")
	mismatch := !s.LiveKnown
	if s.LiveKnown {
		live = fmt.Sprintf("live=%s", formatPct(s.LiveVolume))
		if s.LiveMuted {
			live += " (muted)"
		}
		if s.HasInternal && math.Abs(s.LiveVolume-s.InternalVolume) > mismatchEpsilon {
			mismatch = true
		}
	}

	fields := internal + "  " + live
	if mismatch {
		return line + "  " + routingWarnStyle.Render(fields+"  ⚠")
	}
	return line + "  " + routingOKStyle.Render(fields)
}

func formatPct(v float64) string {
	return fmt.Sprintf("%.0f%%", v*100)
}

// truncateName shortens a node name to maxNodeNameLen, keeping the tail
// (the distinguishing part, e.g. "...pci-0000_08_00.6.analog-stereo") since
// PipeWire node names commonly share a long, low-information prefix.
func truncateName(s string) string {
	if len(s) <= maxNodeNameLen {
		return s
	}
	return "…" + s[len(s)-(maxNodeNameLen-1):]
}
