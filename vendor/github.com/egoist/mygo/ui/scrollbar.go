package ui

// Scrollbar is the scroll bar of a view that scrolls itself rather than
// through a scroll container, as a code editor or a terminal does, laid
// out and painted as a scroll container's are, so that every bar of an app
// looks alike and follows the theme (Theme.Scrollbar, ScrollbarWidth and
// ScrollbarStyle).
//
// The view paints the bar with Painter.Scrollbar while it shows it: a
// scroll container's show while the pointer is over the container or
// drags a thumb, and maps a drag of the thumb to an offset with Thumb and
// OffsetAt.
//
//	bar := ui.Scrollbar{Box: r, View: viewH, Content: contentH, Offset: scrollY}
//	if hovered || dragging {
//		p.Scrollbar(bar, ui.ScrollbarState{Hovered: hovered, Dragging: dragging})
//	}
type Scrollbar struct {
	// Box is the view the bar runs along, at its right edge, or its
	// bottom edge for a horizontal bar, in the coordinates the bar is
	// painted or hit tested in.
	Box Rect
	// Horizontal makes the bar run along the bottom, scrolling sideways.
	Horizontal bool
	// View is the length of the content the view shows, Content the
	// length of all of it, and Offset how far it is scrolled, in any
	// unit, as rows of a terminal's scrollback: the bar shows nothing
	// unless Content is longer than View.
	View, Content, Offset float32
	// Style is how the bar looks: its track and thumb colors, widths,
	// corners and shortest thumb. Its zero fields follow the theme, as
	// Theme.ScrollbarStyle does; a view that paints its own bar sets it
	// to the style it resolves, so that Thumb and OffsetAt lay out the
	// same thumb Painter.Scrollbar paints.
	Style ScrollbarStyle
}

// ScrollbarStyle is how scroll bars look: the widths of their bars and
// thumbs, how round their corners are, the shortest thumb, and the colors
// of their tracks and thumbs at rest, hovered and dragged. A zero field
// follows the theme: Theme.ScrollbarStyle, and Theme.Scrollbar and
// Theme.ScrollbarWidth for the thumb's color and width.
//
// Set it in a theme for every scroll bar of the app, on a scroll container
// (Element.ScrollbarStyle) for that container's, or on a view that paints
// its own bar (Scrollbar.Style), as the code editor's State.Scrollbar
// does.
//
//	// As VS Code's editor: a 14 DIP square thumb in a 14 DIP bar, flush
//	// with its ends.
//	t.ScrollbarStyle = ui.ScrollbarStyle{
//		TrackWidth: 14, ThumbWidth: 14, Square: true, Flush: true, MinLength: 20,
//		Thumb: ui.RGBA(121, 121, 121, 0.4),
//		Hover: ui.RGBA(100, 100, 100, 0.7),
//		Active: ui.RGBA(191, 191, 191, 0.4),
//	}
type ScrollbarStyle struct {
	// TrackWidth is the width of the bar's track and hit area, in DIPs. 0 is
	// ThumbWidth + 6, the thumb with 3 DIPs on either side.
	TrackWidth float32
	// ThumbWidth is the width of the thumb, in DIPs. 0 is the theme's
	// ScrollbarWidth, 6 by default.
	ThumbWidth float32
	// Radius rounds the thumb's corners, in DIPs. 0 is half ThumbWidth,
	// unless Square.
	Radius float32
	// Square leaves the thumb's corners square, as VS Code's scroll bar
	// does. A Radius set with it rounds them instead.
	Square bool
	// Flush leaves no gap between the thumb and the ends of its track, as
	// VS Code's scroll bar; otherwise it keeps 2 DIPs clear at each end.
	Flush bool
	// MinLength is the shortest the thumb gets, in DIPs. 0 is 24.
	MinLength float32
	// Track fills the bar behind the thumb; zero paints none.
	Track Color
	// Thumb colors the thumb at rest, Hover while the pointer is over the
	// bar, and Active while it is dragged. Zero Thumb takes the theme's
	// Scrollbar; zero Hover and Active take Thumb.
	Thumb, Hover, Active Color
}

// Resolve returns s with its zero fields filled from t, as Painter.Scrollbar
// and scroll containers paint it: TrackWidth is ThumbWidth + 6, ThumbWidth
// the theme's ScrollbarWidth, Radius half ThumbWidth (or 0 with Square),
// MinLength 24, Thumb the theme's Scrollbar, and Hover and Active Thumb. A
// nil t is a zero theme.
func (s ScrollbarStyle) Resolve(t *Theme) ScrollbarStyle {
	if t == nil {
		t = &Theme{}
	}
	// The theme's style comes first; s overrides what it sets.
	base := t.ScrollbarStyle
	if s.TrackWidth > 0 {
		base.TrackWidth = s.TrackWidth
	}
	if s.ThumbWidth > 0 {
		base.ThumbWidth = s.ThumbWidth
	}
	if s.Square {
		base.Square, base.Radius = true, 0
	}
	if s.Radius != 0 {
		base.Radius, base.Square = s.Radius, false
	}
	if s.Flush {
		base.Flush = true
	}
	if s.MinLength > 0 {
		base.MinLength = s.MinLength
	}
	if s.Track != (Color{}) {
		base.Track = s.Track
	}
	if s.Thumb != (Color{}) {
		base.Thumb = s.Thumb
	}
	if s.Hover != (Color{}) {
		base.Hover = s.Hover
	}
	if s.Active != (Color{}) {
		base.Active = s.Active
	}
	thumbWidth := base.ThumbWidth
	if thumbWidth <= 0 {
		thumbWidth = t.scrollbarWidth()
	}
	width := base.TrackWidth
	if width <= 0 {
		width = thumbWidth + 6
	}
	thumbWidth = min(thumbWidth, width)
	radius := base.Radius
	switch {
	case radius > 0:
	case base.Square:
		radius = 0
	default:
		radius = thumbWidth / 2
	}
	minLength := base.MinLength
	if minLength <= 0 {
		minLength = 24
	}
	thumb := base.Thumb
	if thumb == (Color{}) {
		thumb = t.Scrollbar
	}
	hover, active := base.Hover, base.Active
	if hover == (Color{}) {
		hover = thumb
	}
	if active == (Color{}) {
		active = thumb
	}
	return ScrollbarStyle{
		TrackWidth: width, ThumbWidth: thumbWidth, Radius: radius, Square: base.Square, Flush: base.Flush,
		MinLength: minLength, Track: base.Track, Thumb: thumb, Hover: hover, Active: active,
	}
}

// ScrollbarState is how a Scrollbar paints: Dragging while the pointer
// drags its thumb, which widens it, as a scroll container's; Hovered while
// the pointer is over the bar, which colors it as Hover does; Color, if
// not zero, colors it for a view whose colors are not the theme's, as a
// terminal's, in place of the style's thumb colors.
type ScrollbarState struct {
	Dragging bool
	Hovered  bool
	Color    Color
}

// Scrolls reports whether the bar has anywhere to scroll, and so shows.
func (s Scrollbar) Scrolls() bool { return s.Content > s.View+0.5 }

// along returns where the track starts along the bar, and its length.
func (s Scrollbar) along() (pos, track float32) {
	if s.Horizontal {
		return s.Box.X, max(s.Box.W, 0)
	}
	return s.Box.Y, max(s.Box.H, 0)
}

// Thumb returns where the thumb starts along the bar, as Box's X for a
// horizontal bar, else its Y, and how long it is.
func (s Scrollbar) Thumb() (start, length float32) {
	t := s.thumb(s.Style)
	return t.Y, t.H
}

// thumb returns the thumb's box along the bar, as style lays it out.
func (s Scrollbar) thumb(style ScrollbarStyle) Rect {
	pos, track := s.along()
	return scrollThumb(pos, track, s.View, max(s.Content, s.View), s.Offset, style.MinLength, style.inset())
}

// OffsetAt returns the offset that brings the thumb's start to start, as
// the thumb is dragged, within the content.
func (s Scrollbar) OffsetAt(start float32) float32 {
	pos, track := s.along()
	_, length := s.Thumb()
	inset := s.Style.inset()
	travel := track - 2*inset - length
	reach := s.Content - s.View
	if travel <= 0 || reach <= 0 {
		return 0
	}
	return max(0, min((start-pos-inset)/travel, 1)) * reach
}

// inset is the gap between the thumb and each end of its track.
func (s ScrollbarStyle) inset() float32 {
	if s.Flush {
		return 0
	}
	return 2
}

// trackRect returns the track's box, the width of style across.
func (s Scrollbar) trackRect(style ScrollbarStyle) Rect {
	if s.Horizontal {
		return Rect{s.Box.X, s.Box.Y + s.Box.H - style.TrackWidth, s.Box.W, style.TrackWidth}
	}
	return Rect{s.Box.X + s.Box.W - style.TrackWidth, s.Box.Y, style.TrackWidth, s.Box.H}
}

// thumbRect returns the thumb's box, the width of style.ThumbWidth across,
// in the middle of the bar.
func (s Scrollbar) thumbRect(style ScrollbarStyle) Rect {
	t := s.thumb(style)
	across := (style.TrackWidth - style.ThumbWidth) / 2
	if s.Horizontal {
		return Rect{t.Y, s.Box.Y + s.Box.H - style.TrackWidth + across, t.H, style.ThumbWidth}
	}
	return Rect{s.Box.X + s.Box.W - style.TrackWidth + across, t.Y, style.ThumbWidth, t.H}
}

// Scrollbar paints the track and thumb of a scroll bar, as a scroll
// container's, if it has anywhere to scroll.
func (p *Painter) Scrollbar(s Scrollbar, st ScrollbarState) {
	p.ScrollbarTrack(s)
	p.ScrollbarThumb(s, st)
}

// ScrollbarTrack paints the track of a scroll bar, if its style has one,
// under what a view draws on the bar: a view with marks on its bar paints
// the track, then the marks, then the thumb (ScrollbarThumb).
func (p *Painter) ScrollbarTrack(s Scrollbar) {
	if !s.Scrolls() {
		return
	}
	style := s.Style.Resolve(p.Theme())
	if style.Track.A > 0 {
		p.Fill(s.trackRect(style), style.Track, 0)
	}
}

// ScrollbarThumb paints the thumb of a scroll bar, as a scroll container's,
// if it has anywhere to scroll.
func (p *Painter) ScrollbarThumb(s Scrollbar, st ScrollbarState) {
	if !s.Scrolls() {
		return
	}
	style := s.Style.Resolve(p.Theme())
	c := style.Thumb
	if st.Color != (Color{}) {
		// As faint as the theme's, which High Contrast makes bolder.
		c = st.Color.Alpha(float32(style.Thumb.A) / 255)
		style.Hover, style.Active = c, c
	}
	switch {
	case st.Dragging:
		c = style.Active
	case st.Hovered:
		c = style.Hover
	}
	// A thumb with no room to grow changes color only while dragged, as
	// VS Code's.
	p.scrollThumbFill(s.thumbRect(style), s.Horizontal, st.Dragging && style.ThumbWidth+2 <= style.TrackWidth, c, style.Radius)
}

// scrollThumbFill paints a thumb at bar, widened toward the view while
// dragged, where it has room.
func (p *Painter) scrollThumbFill(bar Rect, horizontal, grow bool, c Color, radius float32) {
	if grow {
		if horizontal {
			bar.Y, bar.H = bar.Y-2, bar.H+2
		} else {
			bar.X, bar.W = bar.X-2, bar.W+2
		}
		radius++
	}
	half := bar.W / 2
	if horizontal {
		half = bar.H / 2
	}
	radius = min(radius, half)
	p.fill(bar, [4]float32{radius, radius, radius, radius}, c, 0, Color{})
}
