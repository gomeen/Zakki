package app

import (
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

const (
	dotDiameter    float32 = 12
	dotGap         float32 = 12
	pillHeight     float32 = 22
	pillPadX       float32 = 11
	bridgeReach    float32 = 10
	pillTitleSize  float32 = 12
	morphPadX      float32 = 24
	tabTrackHeight float32 = 38
)

func clampF(v, lo, hi float32) float32 { return min(max(v, lo), hi) }

const defaultTabTitleWidth = float32(120)

func (a *app) tabTitleWidthLimit() float32 {
	w := a.cfg.TabTitleWidth
	if w <= 0 {
		w = defaultTabTitleWidth
	}
	return clampF(w, 80, 220)
}

func (a *app) tabAnimDuration() time.Duration {
	speed := a.cfg.TabAnimSpeed
	if speed <= 0 {
		speed = 1
	}
	return time.Duration(float32(time.Millisecond) * 420 / clampF(speed, 0.5, 2))
}

func (a *app) glassStrength() float32 {
	s := a.cfg.GlassStrength
	if s <= 0 {
		s = 0.5
	}
	return clampF(s, 0.2, 0.9)
}

type tabSlotGeom struct{ w, cx float32 }

func pillWidthOf(titleW float32) float32 { return pillHeight + max(0, titleW) }

// equalTabWidths 未命名时每个标记一样大，不因选中状态改变槽位宽度。
func equalTabWidths(n int) []float32 {
	widths := make([]float32, n)
	for i := range widths {
		widths[i] = dotDiameter
	}
	return widths
}

// tabSlotWidths 有名字时，当前标签是包住标题的色条，其余仍是同大标记。
func tabSlotWidths(capsuleW float32, n, active int) []float32 {
	widths := equalTabWidths(n)
	if active >= 0 && active < n {
		widths[active] = capsuleW
	}
	return widths
}

func fitTabLayout(room, titleW float32, n, active int) ([]tabSlotGeom, float32, float32) {
	gap := dotGap
	base := float32(n-1)*dotDiameter + pillHeight
	if n > 1 {
		gap = clampF((room-base-titleW)/float32(n-1), 4, dotGap)
	}
	titleW = min(titleW, max(0, room-base-float32(n-1)*gap))
	slots, total := layoutTabSlotsWithGap(tabSlotWidths(pillWidthOf(titleW), n, active), gap)
	return slots, total, titleW
}

func layoutTabSlots(widths []float32) ([]tabSlotGeom, float32) {
	return layoutTabSlotsWithGap(widths, dotGap)
}

func layoutTabSlotsWithGap(widths []float32, gap float32) ([]tabSlotGeom, float32) {
	slots := make([]tabSlotGeom, len(widths))
	var x float32
	for i, w := range widths {
		slots[i] = tabSlotGeom{w: w, cx: x + w/2}
		x += w + gap
	}
	if len(widths) > 0 {
		x -= gap
	}
	return slots, x
}

func easeLiquid(t float32) float32 {
	t = clampF(t, 0, 1)
	u := t - 1
	return 1 + 2.2*u*u*u + 1.2*u*u
}

func interpSlots(from, to []tabSlotGeom, t float32) []tabSlotGeom {
	out := slices.Clone(to)
	for i := range out {
		if i < len(from) {
			out[i] = tabSlotGeom{w: from[i].w + (to[i].w-from[i].w)*t, cx: from[i].cx + (to[i].cx-from[i].cx)*t}
		}
	}
	return out
}

func bridgeWidthFor(gap float32) float32 {
	if gap >= bridgeReach {
		return 0
	}
	t := clampF(1-gap/bridgeReach, 0, 1)
	return t * t * pillHeight * 0.8
}

func edgeGap(pillCx, halfWidth, dotCx, dr float32) float32 {
	return float32(math.Abs(float64(pillCx-dotCx))) - halfWidth - dr
}

const (
	tabTitleFadeOutEnd  = float32(0.35)
	tabTitleFadeInStart = float32(0.55)
)

func titleAlphas(t float32) (float32, float32) {
	return clampF((tabTitleFadeOutEnd-t)/tabTitleFadeOutEnd, 0, 1),
		clampF((t-tabTitleFadeInStart)/(1-tabTitleFadeInStart), 0, 1)
}

func emptyDotInk(dark bool) ui.Color {
	if dark {
		return ui.Hex("#58585c")
	}
	return ui.Hex("#c4c4c8")
}

type tabBody struct {
	cx, w, h  float32
	color     ui.Color
	textColor ui.Color
	title     string
	textW     float32
}

type tabMorphState struct {
	active           bool
	ids              []uint64
	from, to         []tabSlotGeom
	toIdx            int
	fromID, toID     uint64
	fromBody, toBody tabBody
	start            time.Time
	duration         time.Duration
}

func (m tabMorphState) progress(now time.Time) float32 {
	if !m.active || m.duration <= 0 {
		return 1
	}
	return clampF(float32(now.Sub(m.start))/float32(m.duration), 0, 1)
}

func (m tabMorphState) bodyAt(now time.Time) tabBody {
	t := m.progress(now)
	e := easeLiquid(t)
	b := m.fromBody
	b.cx += (m.toBody.cx - b.cx) * e
	b.w += (m.toBody.w - b.w) * e
	b.h += (m.toBody.h - b.h) * e
	colorT := t
	if bodyShowsTitle(m.fromBody) || bodyShowsTitle(m.toBody) {
		// 有字时保持端点配色，只在标题全隐阶段换底色，避免黑白字可见时跳色。
		colorT = clampF((t-tabTitleFadeOutEnd)/(tabTitleFadeInStart-tabTitleFadeOutEnd), 0, 1)
	}
	b.color = b.color.Mix(m.toBody.color, colorT)
	if t >= 0.5 {
		b.title, b.textW, b.textColor = m.toBody.title, m.toBody.textW, m.toBody.textColor
	}
	if t >= tabTitleFadeOutEnd && t <= tabTitleFadeInStart {
		// 中途被下一次切换接管时，不把已淡出的标题重新显示在过渡底色上。
		b.title, b.textW = "", 0
	}
	return b
}

func (a *app) cancelMorphFor(id uint64) {
	if a.morph.active && slices.Contains(a.morph.ids, id) {
		a.morph = tabMorphState{}
	}
}

func (a *app) animClock(p *ui.Painter) time.Time {
	if a.animNow != nil {
		return a.animNow()
	}
	return p.Now()
}

func (a *app) uiNow(c *ui.Context) time.Time {
	if a.animNow != nil {
		return a.animNow()
	}
	return c.Now()
}

func sameSlots(a, b []tabSlotGeom) bool { return slices.Equal(a, b) }

func (a *app) syncTabMorph(now time.Time, reduceMotion bool, slots []tabSlotGeom, ids []uint64, body tabBody) {
	activeID := a.tabs[a.active].ID
	defer func() {
		a.lastSlots = slices.Clone(slots)
		a.lastIDs = slices.Clone(ids)
		a.builtActiveID = activeID
		a.lastBody = body
	}()
	// 减少动态效果（系统）或用户关闭顶栏动效时直接切换，不做液态补间。
	if reduceMotion || a.cfg.TabMotionOff || !slices.Equal(a.lastIDs, ids) || a.builtActiveID == 0 {
		a.morph = tabMorphState{}
		return
	}
	if activeID == a.builtActiveID {
		if !sameSlots(a.lastSlots, slots) || body != a.lastBody || a.morph.progress(now) >= 1 {
			a.morph = tabMorphState{}
		}
		return
	}
	from, fromBody := a.lastSlots, a.lastBody
	if a.morph.active {
		from = interpSlots(a.morph.from, a.morph.to, easeLiquid(a.morph.progress(now)))
		fromBody = a.morph.bodyAt(now)
	}
	a.morph = tabMorphState{
		active: true, ids: slices.Clone(ids), from: slices.Clone(from), to: slices.Clone(slots),
		toIdx: a.active, fromID: a.builtActiveID, toID: activeID,
		fromBody: fromBody, toBody: body, start: now, duration: a.tabAnimDuration(),
	}
}

func capsulePath(x, y, w, h float32) ui.Path {
	var p ui.Path
	r := h / 2
	const k = 0.5522847
	kk := r * k
	p.MoveTo(x+r, y)
	p.LineTo(x+w-r, y)
	p.CubeTo(x+w-r+kk, y, x+w, y+r-kk, x+w, y+r)
	p.LineTo(x+w, y+h-r)
	p.CubeTo(x+w, y+h-r+kk, x+w-r+kk, y+h, x+w-r, y+h)
	p.LineTo(x+r, y+h)
	p.CubeTo(x+r-kk, y+h, x, y+h-r+kk, x, y+h-r)
	p.LineTo(x, y+r)
	p.CubeTo(x, y+r-kk, x+r-kk, y, x+r, y)
	p.Close()
	return p
}

// The bridge meets the circular caps tangentially; its waist narrows with distance.
func liquidBridge(body tabBody, dotCX, cy float32) (ui.Path, bool) {
	gap := edgeGap(body.cx, body.w/2, dotCX, dotDiameter/2)
	var p ui.Path
	if gap < -dotDiameter/2 || gap >= bridgeReach {
		return p, false
	}
	dir := float32(1)
	if dotCX < body.cx {
		dir = -1
	}
	pr, dr := body.h/2, dotDiameter/2
	capCX := body.cx + dir*(body.w/2-pr)
	strength := clampF(1-max(0, gap)/bridgeReach, 0, 1)
	angle := float64(strength * 0.9)
	co, si := float32(math.Cos(angle)), float32(math.Sin(angle))
	x0, x1 := capCX+dir*pr*co, dotCX-dir*dr*co
	y0, y1 := pr*si, dr*si
	span := float32(math.Abs(float64(x1 - x0)))
	waist := min(y0, y1) * strength * 0.4
	midX := (x0 + x1) / 2
	control := min(span*0.25, (min(y0, y1)-waist)/co*0.8)
	p.MoveTo(x0, cy-y0)
	p.CubeTo(x0+dir*control*si, cy-y0+control*co, midX-dir*span*0.15, cy-waist, midX, cy-waist)
	p.CubeTo(midX+dir*span*0.15, cy-waist, x1-dir*control*si, cy-y1+control*co, x1, cy-y1)
	p.LineTo(x1, cy+y1)
	p.CubeTo(x1-dir*control*si, cy+y1-control*co, midX+dir*span*0.15, cy+waist, midX, cy+waist)
	p.CubeTo(midX-dir*span*0.15, cy+waist, x0+dir*control*si, cy+y0-control*co, x0, cy+y0)
	p.Close()
	return p, true
}

func (a *app) morphDotInk(id uint64, dark bool) ui.Color {
	for i := range a.tabs {
		if a.tabs[i].ID == id {
			return a.tabInk(i, dark)
		}
	}
	return emptyDotInk(dark)
}

func paintSolidDot(p *ui.Painter, cx, cy float32, ink ui.Color) {
	var path ui.Path
	path.Circle(cx, cy, dotDiameter/2)
	p.FillPath(&path, ink)
}

// ---- 标签圆点形状 ----

// 只保留圆点与圆环。其余形状和液态滑动叠在一起会显得突兀，
// 旧文件里的其他值在载入时回退成圆点。
const (
	tabDotShapeDot  = "dot"  // 圆点（默认）
	tabDotShapeRing = "ring" // 圆环（描边，不参与液态桥接）
)

func isValidTabDotShape(id string) bool {
	switch id {
	case "", tabDotShapeDot, tabDotShapeRing:
		return true
	}
	return false
}

// activeDotShape 当前生效的形状 ID（空串 = 默认圆点）。
func (a *app) activeDotShape() string {
	if isValidTabDotShape(a.cfg.TabDotShape) && a.cfg.TabDotShape != "" {
		return a.cfg.TabDotShape
	}
	return tabDotShapeDot
}

// paintTabDot 绘制一枚与其他标签同大的标记。圆环描边，圆点填充。
func paintTabDot(p *ui.Painter, cx, cy float32, ink ui.Color, shape string) {
	if shape == tabDotShapeRing {
		var path ui.Path
		path.Circle(cx, cy, dotDiameter/2-1.2)
		p.StrokePath(&path, 2.4, ink)
		return
	}
	paintSolidDot(p, cx, cy, ink)
}

// muteTabInk 把非当前标签压淡。尺寸不变，只靠深浅区分当前标签。
func muteTabInk(ink ui.Color, dark bool) ui.Color {
	base := ui.RGB(255, 255, 255)
	if dark {
		base = ui.RGB(28, 28, 30)
	}
	return ink.Mix(base, 0.58)
}

// activeDotInk 当前标记的着色：浅色调色板（黄、浅粉等高亮度色）在浅色顶栏上
// 对比不足，向黑色收敛最多 18% 提高对比；深色与深色主题原样。
func activeDotInk(color ui.Color, dark bool) ui.Color {
	if dark {
		return color
	}
	lum := 0.2126*float64(color.R) + 0.7152*float64(color.G) + 0.0722*float64(color.B)
	if lum <= 168 {
		return color
	}
	return color.Mix(ui.RGB(0, 0, 0), 0.18)
}

func paintBody(p *ui.Painter, body tabBody, ink ui.Color, cy float32) {
	path := capsulePath(body.cx-body.w/2, cy-body.h/2, body.w, body.h)
	p.FillPath(&path, ink)
}

func paintBodyTitle(p *ui.Painter, body tabBody, cy float32, title string, textW, alpha float32) {
	if title == "" || alpha <= 0 {
		return
	}
	r := ui.Rect{X: body.cx - body.w/2 + pillPadX, Y: cy - body.h/2, W: max(0, body.w-2*pillPadX), H: body.h}
	p.Clip(r, 0, func() {
		p.Text(body.cx-textW/2, cy-pillTitleSize/2-2, title, pillTitleSize, body.textColor.Alpha(alpha))
	})
}

func bodyShowsTitle(body tabBody) bool {
	return body.title != "" || body.w > dotDiameter+1 || body.h > dotDiameter+1
}

func (a *app) paintTabStrip(p *ui.Painter, r ui.Rect, dark bool) {
	cy := r.Y + r.H/2
	slots, ids, body := a.lastSlots, a.lastIDs, a.lastBody
	m := a.morph
	if m.active {
		slots, ids = interpSlots(m.from, m.to, easeLiquid(m.progress(a.animClock(p)))), m.ids
		body = m.bodyAt(a.animClock(p))
	}
	body.cx += r.X
	shape := a.activeDotShape()
	ink := activeDotInk(body.color, dark)
	if bodyShowsTitle(body) || a.renaming != 0 {
		ink = body.color
	}
	if m.active {
		if shape != tabDotShapeRing {
			for _, s := range slots {
				if bridge, ok := liquidBridge(body, r.X+s.cx, cy); ok {
					p.FillPath(&bridge, ink)
				}
			}
		}
	}
	for i, s := range slots {
		if !m.active && ids[i] == a.builtActiveID {
			continue
		}
		paintTabDot(p, r.X+s.cx, cy, muteTabInk(a.morphDotInk(ids[i], dark), dark), shape)
	}
	if bodyShowsTitle(body) || a.renaming != 0 {
		paintBody(p, body, body.color, cy)
		if a.renaming != 0 {
			return
		}
		if m.active {
			fromA, toA := titleAlphas(m.progress(a.animClock(p)))
			paintBodyTitle(p, body, cy, m.fromBody.title, m.fromBody.textW, fromA)
			paintBodyTitle(p, body, cy, m.toBody.title, m.toBody.textW, toA)
		} else {
			paintBodyTitle(p, body, cy, body.title, body.textW, 1)
		}
		return
	}
	if shape == tabDotShapeRing {
		paintTabDot(p, body.cx, cy, activeDotInk(body.color, dark), shape)
		return
	}
	paintBody(p, body, activeDotInk(body.color, dark), cy)
}

func fitTabTitle(s string, limit float32, measure func(string) float32) (string, float32) {
	if limit <= 0 || s == "" {
		return "", 0
	}
	if w := measure(s); w <= limit {
		return s, w
	}
	if measure("…") > limit {
		return "", 0
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if measure(string(runes[:mid])+"…") <= limit {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	s = string(runes[:lo]) + "…"
	return s, measure(s)
}

func measuredTitleWidth(s string, limit float32, measure func(string) float32) float32 {
	if measure == nil {
		return min(float32(len([]rune(s)))*pillTitleSize, limit)
	}
	return min(measure(s), limit)
}

// ---- 顶栏拖拽排序 ----

// tabDrag 顶栏拖拽中的标签（稳定 ID）；独立类型，其他落点不会误收。
type tabDrag uint64

// tabDrop 待执行的拖拽重排：松手那帧仍在构建标签条，帧末再改标签次序。
type tabDrop struct {
	id  uint64
	to  int
	dir int // 菜单移动方向；0 表示拖拽的绝对插入位
}

// tabDropIndex 标签条局部横坐标 x 对应的插入位（List 语义：移到第 to 个之前）。
func tabDropIndex(slots []tabSlotGeom, x float32) int {
	for i, s := range slots {
		if x < s.cx {
			return i
		}
	}
	return len(slots)
}

// tabDropMarkX 插入位的指示线横坐标：相邻两枚标记空隙的中点，两端外扩半个间距。
func tabDropMarkX(slots []tabSlotGeom, to int) float32 {
	n := len(slots)
	switch {
	case to <= 0:
		return slots[0].cx - slots[0].w/2 - dotGap/2
	case to >= n:
		return slots[n-1].cx + slots[n-1].w/2 + dotGap/2
	}
	l, r := slots[to-1], slots[to]
	return (l.cx + l.w/2 + r.cx - r.w/2) / 2
}

// tabStripDrop 标签条作为拖拽落点：拖动经过时返回插入位供画指示线（不改变
// 次序的位置返回 -1），松手时记下重排，由 runTabMenuPending 帧末执行。
func (a *app) tabStripDrop(strip ui.Element, slots []tabSlotGeom) int {
	over, hovering := ui.DragOver[tabDrag](strip)
	dropped, ok := ui.Drop[tabDrag](strip)
	if !hovering && !ok || len(slots) != len(a.tabs) {
		return -1
	}
	x, _, _ := strip.PointerPosition()
	to := tabDropIndex(slots, x)
	if ok {
		a.dropPending = tabDrop{id: uint64(dropped), to: to}
		return -1
	}
	if i := a.tabIndexByID(uint64(over)); i < 0 || to == i || to == i+1 {
		return -1
	}
	return to
}

// ---- 标签右键菜单 ----

// tabContextMenu 标签右键菜单：重命名、前后移动、导出与删除，动作与
// 设置页/菜单栏/快捷键复用同一标签操作；删除前加分隔线（破坏性项独立成组）。
// 只剩一个标签时禁用删除，首尾标签分别禁用越界移动。
// 动作记为待执行、帧末跑（Chosen 在选中后的下一帧报告），不在构建期改状态。
func (a *app) tabContextMenu(i int) func(*ui.Menu) {
	return func(m *ui.Menu) {
		tabID := a.tabs[i].ID
		rename := m.Item(locales.MenuRenameTab())
		if b, ok := parsedFixedBinding("renameTab"); ok {
			rename.Shortcut(b.mods, b.key)
		}
		earlier := m.Item(locales.TabMoveEarlier()).Disabled(i <= 0)
		later := m.Item(locales.TabMoveLater()).Disabled(i >= len(a.tabs)-1)
		export := m.Item(locales.MenuExportMarkdown())
		m.Separator()
		// Danger 只影响窗口内绘制的菜单（0.8 起）：系统菜单没有该属性，
		// 删除仍靠独立分组与文字说明保持可识别。
		dele := m.Item(locales.MenuDeleteTab()).Disabled(len(a.tabs) <= 1).Danger()
		if b, ok := parsedFixedBinding("deleteTab"); ok {
			dele.Shortcut(b.mods, b.key)
		}
		if rename.Chosen() {
			a.menuRenamePending = tabID
		}
		if earlier.Chosen() {
			a.dropPending = tabDrop{id: tabID, dir: -1}
		}
		if later.Chosen() {
			a.dropPending = tabDrop{id: tabID, dir: 1}
		}
		if dele.Chosen() {
			a.menuDeletePending = tabID
		}
		if export.Chosen() {
			a.menuExportPending = tabID
		}
	}
}

// runTabMenuPending 帧末执行右键菜单动作与顶栏拖拽重排；目标按稳定 ID 重新定位，菜单
// 打开期间标签被删时静默丢弃。重命名前先切到该标签：重命名输入框画在
// 当前标签的色条里，与双击标签的「先切换再重命名」同路径。
func (a *app) runTabMenuPending(c *ui.Context) {
	renamed, deleted, exported := a.menuRenamePending, a.menuDeletePending, a.menuExportPending
	a.menuRenamePending, a.menuDeletePending, a.menuExportPending = 0, 0, 0
	if drop := a.dropPending; drop.id != 0 {
		a.dropPending = tabDrop{}
		if i := a.tabIndexByID(drop.id); i >= 0 {
			moved := false
			if drop.dir != 0 {
				moved = a.moveTab(i, drop.dir)
			} else {
				moved = a.reorderTab(i, drop.to)
			}
			if moved {
				c.Invalidate()
			}
		}
	}
	if exported != 0 {
		if i := a.tabIndexByID(exported); i >= 0 {
			a.exportTab(i)
		}
	}
	if renamed != 0 {
		if i := a.tabIndexByID(renamed); i >= 0 {
			a.switchTab(i)
			a.beginRename(i)
			c.Invalidate()
		}
	}
	if deleted != 0 {
		if i := a.tabIndexByID(deleted); i >= 0 {
			a.confirmDeleteTab(i)
			c.Invalidate()
		}
	}
}
