package ui

// SplitButtonParts are the parts of a split button (SplitButton,
// PrimarySplitButton): Group joins Button, which does the action, and
// Arrow, which opens a menu of related actions.
type splitButtonParts struct {
	// Group holds the two segments and draws the face and the border they
	// share: Disabled on it disables both, Label names it, and it lays out
	// among its siblings as a button does.
	Group *node
	// Button is the leading segment, which does the action: Clicked
	// reports a click, or Enter or Space while it has the focus, as
	// Button's does. Give it other content with Children and an empty
	// label.
	Button *node
	// Arrow is the trailing segment, showing an arrow: Menu gives it the
	// menu, or PopoverMenuButton one drawn in the window. Assistive
	// technology names it More options until its Label names it otherwise.
	Arrow *node
	c     *context
}

// SplitButton creates a button joined to an arrow that opens a menu of
// related actions, as Save beside Save As…: one control drawn as Button
// is, with the face and the border its two segments share and a line
// between them, each lighting up apart as the pointer hovers and presses
// it. Button, its leading segment, shows label and does the action; give
// Arrow, its trailing segment, the menu with Menu, or with
// PopoverMenuButton for one MyGo draws in the window, which opens below
// the arrow as a menu button's does:
//
//	save := ui.SplitButton(c, "Save")
//	save.Arrow.Menu(func(m *ui.Menu) {
//		if m.Item("Save As…").Chosen() {
//			app.saveAs()
//		}
//	})
//	if save.Button.Clicked() {
//		app.save()
//	}
//
// It is one stop of Tab, and Left and Right move the focus between its
// segments, the other way round right to left, where the arrow is on the
// left. Enter and Space click Button while it has the focus, and open the
// menu while Arrow has it, as do Up and Down; Alt+Down opens it from
// Button too. Assistive technology sees a group of a button and a menu
// button, named More options until Arrow's Label names it, expanded while
// its menu shows. Disabled on Group disables the whole control, and on
// Button the action alone, leaving the menu. In a toolbar it takes no face
// of its own until hovered, as the toolbar's buttons. PrimarySplitButton
// is one in the accent color.
func coreSplitButton(c *context, label string) splitButtonParts {
	return splitButton(c, label, false)
}

// PrimarySplitButton creates a split button (SplitButton) in the accent
// color, for the main action, as PrimaryButton is.
func corePrimarySplitButton(c *context, label string) splitButtonParts {
	return splitButton(c, label, true)
}

func splitButton(c *context, label string, primary bool) splitButtonParts {
	t := c.theme
	g := coreRow(c).AlignItems(Stretch).Shrink(0).Role(RoleGroup)
	g.widget = "SplitButton"
	g.FocusGroup(Horizontal)
	p := splitButtonParts{Group: g, c: c}
	var line *node
	g.Children(func() {
		p.Button = button(c, label, primary)
		line = coreBox(c).Width(1).Shrink(0)
		p.Arrow = coreButtonBase(c)
		styleButton(c, p.Arrow, primary)
	})
	act, arrow := p.Button, p.Arrow
	// The segments are built as buttons; the group takes their face and
	// border, so that it is as large as a button, and they only light up
	// over it.
	base, border := act.bg, act.borderC
	var bw float32
	if act.hasBorder() {
		bw = act.border[0]
		g.Border(bw, border)
	}
	g.Radius(t.Radius).Background(base)
	emphasized := primary && c.emphasized(t)
	line.Background(t.Border)
	ink := t.TextMuted
	if emphasized {
		line.Background(t.Accent.Mix(t.AccentText, 0.35))
		ink = t.AccentText
	}
	arrow.Label(c.texts().MoreOptions()).PaddingX(t.Space(2))
	if primary && !emphasized {
		// Its border inside the padding, as PrimaryButton's, so that it is
		// as large as in an active window: on its outer sides, where the
		// border is.
		act.PaddingEnd(t.Space(3.5))
		arrow.PaddingEnd(t.Space(2) - 1)
	}
	arrow.Children(func() { chevron(c, ink) })
	// Square where they meet, rounded inside the border elsewhere.
	r := max(t.Radius-bw, 0)
	start, end := [4]float32{r, 0, 0, r}, [4]float32{0, r, r, 0}
	if g.rtl() {
		start, end = end, start
	}
	act.radius, arrow.radius = start, end
	for _, s := range [2]*node{act, arrow} {
		s.Border(0, Color{}).Background(Color{}).FocusRing(false)
		s.faces[0] = Color{}
	}
	if c.buttons == toolbarButtons {
		// No face until the pointer is over it, then the face of a
		// toolbar's hovered button, the segment under the pointer a shade
		// deeper.
		g.faces[0], g.faces[1] = base, t.SurfaceHover
		g.styleFn = styleSplitGroup
		line.styleFn = styleSplitLine
		hover := t.SurfaceHover.Mix(t.SurfacePressed, 0.5)
		act.faces[1], arrow.faces[1] = hover, hover
	}
	// Above both segments, the ring of the one with the focus goes around
	// the border on its outer sides, as round as it, as a button's ring
	// goes around its own border.
	g.DrawOver(func(pt *Painter, _ Rect) {
		for _, s := range [2]*node{act, arrow} {
			if !s.FocusVisible() || s.IsDisabled() {
				continue
			}
			box := Rect{s.x, s.y - bw, s.w + bw, s.h + 2*bw}
			radius := [4]float32{0, t.Radius, t.Radius, 0}
			if s.x == min(act.x, arrow.x) {
				// The segment on the left.
				box.X -= bw
				radius = [4]float32{t.Radius, 0, 0, t.Radius}
			}
			pt.focusRingOf(s, box, radius)
		}
	})
	if act.Shortcut(Alt, KeyDown) && arrow.hasState() {
		// As on a menu button, the menu opens from the keyboard on its
		// first item.
		c.rt.openMenuButton(arrow.st, true)
	}
	return p
}

// styleSplitGroup is the styleFn of a split button in a toolbar: the face
// of a hovered button while the pointer is over it, none otherwise.
func styleSplitGroup(g *node) {
	if g.bg == g.faces[0] && g.Hovered() {
		g.bg = g.faces[1]
	}
}

// styleSplitLine is the styleFn of the line between a split button's
// segments in a toolbar, which shows with the face (styleSplitGroup).
func styleSplitLine(l *node) {
	if p := l.parent; p == nil || !p.Hovered() {
		l.bg = Color{}
	}
}
