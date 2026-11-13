package ui

// ListRow gives a row its shared context, index, selection and list focus,
// and what the pointer does to it: content drawing the row's hover, press
// or a ripple from the pointer reads them here, as the row's Element would
// report them, and leaves the row the one element taking the click.
type ListRow struct {
	Context  *Context
	Index    int
	owner    Element
	row      Element
	selected bool
}

func (r ListRow) Selected() bool    { return r.owner.Valid() && r.selected }
func (r ListRow) ListFocused() bool { return r.owner.FocusWithin() }

// Hovered reports whether the pointer is over the row, as Element.Hovered
// does for the row: content asking for it is built again as the pointer
// comes and goes, while the row keeps taking the input.
func (r ListRow) Hovered() bool { return r.row.Hovered() }

// Pressed reports whether the pointer's primary button presses the row,
// as Element.Pressed does for the row, which takes the press: content
// asking for it draws the press without taking it, so a click on the row,
// or Tester.Click on its ListState.Label, still chooses it. In a list
// that does not choose its rows, asking makes the row take the press.
func (r ListRow) Pressed() bool { return r.row.Pressed() }

// PointerPosition returns the pointer's position relative to the row's
// box and whether it is over the row, as Element.PointerPosition does for
// the row: a ripple starts where Pressed first reports the press.
//
//	if row.Pressed() && app.ripple.Row != row.Index {
//		x, y, _ := row.PointerPosition()
//		app.ripple = Ripple{Row: row.Index, X: x, Y: y, At: time.Now()}
//	}
func (r ListRow) PointerPosition() (x, y float32, over bool) { return r.row.PointerPosition() }

type listSpec struct {
	state *ListState
	count int
	built bool
}

// List creates a virtual list. The original row callback remains supported.
// Omit it to configure ItemKey and Selection, then call Rows.
func List(c *Context, s *ListState, n int, row ...func(int)) Element {
	raw := c.build()
	if raw == nil {
		return Element{}
	}
	if len(row) > 1 {
		panic("ui: List accepts one row builder")
	}
	if len(row) == 1 {
		// List is a public constructor, as the generated ones: its input
		// reaches Changed as it returns, also in a container still
		// building, as a split's pane.
		raw.rt.beginBuild()
		e := coreList(raw, s, n, row[0])
		raw.rt.endBuild()
		return wrapElement(e)
	}
	e := coreScroll(raw)
	e.widget, e.role = "List", RoleList
	if s == nil {
		s = coreLocal(e, "list", func() ListState { return ListState{} })
	}
	*valueBinding[listSpec](e) = listSpec{state: s, count: n}
	return wrapElement(e)
}
func (e Element) ItemKey(key func(int) any) Element {
	if n := e.node(); n != nil {
		if s, ok := n.st.valueBinding.(*listSpec); ok {
			s.state.Key = key
		}
	}
	return e
}
func (e Element) Selection(selection Selector) Element {
	if n := e.node(); n != nil {
		if s, ok := n.st.valueBinding.(*listSpec); ok {
			s.state.Selection = selection
		}
	}
	return e
}
func (e Element) Rows(row func(ListRow)) Element {
	n := e.node()
	if n == nil {
		return e
	}
	s, ok := n.st.valueBinding.(*listSpec)
	if !ok {
		return e
	}
	if s.built {
		panic("ui: List.Rows called twice in one pass")
	}
	s.built = true
	// Rows builds the list: it is the constructor, as List with a row.
	n.c.rt.beginBuild()
	buildList(n.c, n, n, s.state, s.count, func(i int) {
		if row == nil {
			return
		}
		r := ListRow{Context: makeContext(n.c), Index: i, owner: e}
		if rb := n.c.row; rb != nil {
			r.row, r.selected = wrapElement(rb.e), rb.chosen
		}
		row(r)
	}, plainList)
	n.c.rt.endBuild()
	return e
}
