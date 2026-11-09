package app

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

func styleHeading(h ui.Element, level int, md resolvedStyle) {
	level = clamp(level, 1, 6)
	h.Font(md.headingFont).FontSize(md.headingSizes[level-1]).LineHeight(1.25).Bold()
}

// Separate the checkbox hit target from the editable text. Clicking the text
// edits the task; clicking the square toggles it, even while its text has focus.
func (a *app) taskCheckbox(c *ui.Context, blk liveBlock, md resolvedStyle) {
	toggle := func() { a.toggleTask(blk) }
	key := c.Key("task-" + strconv.Itoa(blk.start))
	label := locales.EditorTaskDone(a.previewProjection(blk).text)
	if md.sketch {
		sketchCheckbox(c, key, label, blk.checked, md, uint32(blk.start), toggle)
		return
	}
	checked := blk.checked
	ui.Checkbox(key, &checked, "").Label(label).OnChange(toggle)
}

func (a *app) recordProjectedKey(ev ui.InputEvent, p liveProjection) bool {
	if ev.Kind == ui.InputCommand && ev.Text == "paste" {
		a.nextEditIsPaste = true // 粘贴是独立撤销事务
	}
	if a.mdComposing {
		return false
	}
	// macOS Emacs 键位：⌃P/⌃N（可带 Shift 选区扩展）等价上下方向键。原生编辑器
	// 虽有同样翻译，但仅限事件直达 editor.key() 的路径；在此先归一成箭头键，块
	// 边界提示与块内原生移动共用既有语义。仅 darwin：Windows 的 Ctrl+P/N 惯例是
	// 打印/新建，不绑。
	if runtime.GOOS == "darwin" && ev.Kind == ui.InputKeyDown &&
		(ev.Mods == ui.Ctrl || ev.Mods == ui.Ctrl|ui.Shift) {
		switch ev.Key {
		case ui.KeyP:
			ev.Key, ev.Mods = ui.KeyUp, ev.Mods&ui.Shift
		case ui.KeyN:
			ev.Key, ev.Mods = ui.KeyDown, ev.Mods&ui.Shift
		}
	}
	// 文档级全选（菜单 InputCommand 或主修饰键+A）：表格单元格内保持原生全选。
	if !p.cell {
		if ev.Kind == ui.InputCommand && ev.Text == "selectAll" ||
			(ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyA && ev.Mods == ui.Cmd) {
			a.selectDocWide()
			return true
		}
		if a.selDoc && a.docSelectionInput(ev) {
			return true
		}
	}
	if ev.Kind == ui.InputCommand && (ev.Text == "undo" || ev.Text == "redo") {
		a.liveUndoPending = 1
		if ev.Text == "redo" {
			a.liveUndoPending = -1
		}
		return true
	}
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyZ &&
		(ev.Mods == ui.Super || ev.Mods == ui.Ctrl || ev.Mods == ui.Super|ui.Shift || ev.Mods == ui.Ctrl|ui.Shift) {
		a.liveUndoPending = 1
		if ev.Mods&ui.Shift != 0 {
			a.liveUndoPending = -1
		}
		return true
	}
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyTab && p.cell && (ev.Mods == 0 || ev.Mods == ui.Shift) {
		a.tableBackward = ev.Mods&ui.Shift != 0
		a.keyHint = ui.KeyTab
		return true
	}
	// 代码块内 Tab 不再交给引擎移动焦点（会跳出编辑器落到标签栏），
	// 改记提示键，下一帧在已发布光标上做缩进编辑。
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyTab && !p.cell && (ev.Mods == 0 || ev.Mods == ui.Shift) {
		if blocks := a.mdBlocks(); a.mdActive >= 0 && a.mdActive < len(blocks) && blocks[a.mdActive].kind == bkCode {
			a.tableBackward = ev.Mods&ui.Shift != 0
			a.keyHint = ui.KeyTab
			return true
		}
	}
	if ev.Kind == ui.InputKeyDown && ev.Mods == 0 && ev.Key == ui.KeyBackspace &&
		p.visibleOffset(a.mdCaret) == 0 && a.mdSelEnd <= a.mdCaret {
		// Backspace cannot delete invisible syntax or a table border.
		if p.cell && a.activeTab().Text == a.autoTableAfter && a.mdCaret == a.autoTableCaret {
			a.tableUndoPending = true
		} else if !p.cell {
			a.keyHint = ui.KeyBackspace
		}
		return true
	}
	return a.recordKeyHint(ev)
}

func (a *app) applyProjectedHint(key ui.Key, p liveProjection, start, end int) {
	s, e := p.sourceOffset(start), p.sourceOffset(end)
	if p.cell {
		// Table Enter and Tab retain the existing row/navigation semantics;
		// other keys remain within the cell instead of escaping through pipes.
		if key == ui.KeyEnter || key == ui.KeyTab {
			a.applyKeyHint(key, s, e)
		}
		return
	}
	if key == ui.KeyTab {
		if blocks := a.mdBlocks(); a.mdActive >= 0 && a.mdActive < len(blocks) && blocks[a.mdActive].kind == bkCode {
			backward := a.tableBackward
			a.tableBackward = false
			a.applyCodeTab(backward, p, s, e)
			return
		}
	}
	if key == ui.KeyEnter {
		blk := a.mdBlocks()[a.mdActive]
		if blk.kind == bkPara || blk.kind == bkHeading || blk.kind == bkList || blk.kind == bkTask {
			if len(p.units) == 0 {
				a.afterEnter(p.sourceOffset(start), p.sourceOffset(end))
				return
			}
			if a.createTableOnEnter(s, e, blk) {
				return
			}
			left, right := p.fragment(0, start), p.fragment(end, len(p.units))
			prefix := p.raw[:runeToByte(p.raw, p.start)]
			a.mdDraft = prefix + left
			caret := utf8.RuneCountInString(a.mdDraft)
			if blk.kind == bkList || blk.kind == bkTask {
				if start == 0 && start == end {
					a.mdDraft += "\n" + prefix + right
				} else {
					continuation := listContinuePrefix(blk)
					a.mdDraft += "\n" + continuation + right
					caret += 1 + utf8.RuneCountInString(continuation)
				}
			} else {
				a.mdDraft += "\n\n" + right
				caret += 2
			}
			a.publishDraft(caret)
			return
		}
	}
	switch key {
	case ui.KeyUp, ui.KeyLeft, ui.KeyBackspace:
		if start == 0 {
			s = 0
		}
	case ui.KeyDown, ui.KeyRight:
		if start == len(p.units) {
			s = utf8.RuneCountInString(p.raw)
		}
	case ui.KeyEnter:
		if start == 0 && (a.mdBlocks()[a.mdActive].kind == bkList || a.mdBlocks()[a.mdActive].kind == bkTask) {
			s = 0
		}
	}
	if start == end {
		e = s
	}
	a.applyKeyHint(key, s, e)
}

func (a *app) editTable(c *ui.Context, t *ui.Theme, blk liveBlock) {
	md := a.resolveStyle(-1, themeIsDark(t), true)
	ui.Column(c.Key("editable-table")).FillWidth().Gap(4).Children(func() {
		a.tableToolbar(c, t, blk)
		a.tableGrid(c, t, md, blk, true)
	})
}

func tableCellAt(cells []tableCell, caret int) int {
	idx := 0
	for i, cell := range cells {
		if cell.start <= caret {
			idx = i
		}
	}
	return idx
}

func (a *app) tableGrid(c *ui.Context, t *ui.Theme, md resolvedStyle, blk liveBlock, editing bool) {
	cols := len(blk.table.align)
	if cols == 0 {
		return
	}
	preview := a.previewTable(blk)
	positions := preview.positions
	pos := a.mdCaret
	if a.selSyncStart >= 0 {
		pos = a.selSyncStart
	}
	target := -1
	if editing {
		target = tableCellAt(positions, pos)
	}
	blockIndex := a.mdActive
	if !editing {
		for i, b := range a.mdBlocks() {
			if b.start == blk.start {
				blockIndex = i
				break
			}
		}
	}
	// 每行首个单元格在 positions 里的序号：虚拟化时跳过的行也要算进去。
	firstCell := make([]int, len(blk.table.rows))
	for row, index := 0, 0; row < len(blk.table.rows); row++ {
		firstCell[row] = index
		index += len(blk.table.rows[row])
	}
	pin := -1
	if target >= 0 {
		pin = max(0, sort.SearchInts(firstCell, target+1)-1)
	}
	grid := ui.Column(c).FillWidth()
	if md.sketch {
		// 手账：外框与格线都用铅笔线；单元格不铺底色，避免盖住线条。
		frame := md.textMuted
		grid.Draw(func(p *ui.Painter, r ui.Rect) { sketchRect(p, r, 1.4, frame, uint32(blk.start)) })
	} else {
		grid.Radius(6).Clip().Border(1, t.Border)
	}
	est := md.codeSize*md.lineHeight + 2*md.tablePadY + 1
	grid.Children(func() {
		a.virtualRows(c, virtualRowsKey(blk.raw, bkTable, md), len(blk.table.rows), est, pin, func(row int) ui.Element {
			cells, index := blk.table.rows[row], firstCell[row]
			return ui.Row(c.Key(fmt.Sprintf("row-%d", row))).FillWidth().Children(func() {
				for col := 0; col < cols; col++ {
					ci := index + col
					cell := ui.Box(c.Key(fmt.Sprintf("cell-%d", col))).Grow(1).Basis(0).MinWidth(0).Padding(md.tablePadY, md.tablePadX).
						Label(locales.EditorTableCell(row+1, col+1))
					switch {
					case md.sketch:
						lastRow := row == len(blk.table.rows)-1
						header, left := row == 0, col > 0
						ink := withAlpha(md.textMuted, 0.8)
						seed := uint32(blk.start + row*31 + col*7)
						cell.Draw(func(p *ui.Painter, r ui.Rect) {
							if header {
								highlighter(p, r.X+3, r.Y+r.H*0.55, r.W-6, r.H*0.3, sketchMark(md), seed)
							}
							if !lastRow {
								w := float32(1.1)
								if header {
									w = 1.8 // 表头下画一道更重的线
								}
								sketchLine(p, r.X+2, r.Y+r.H, r.X+r.W-2, r.Y+r.H, w, ink, seed)
							}
							if left {
								sketchLine(p, r.X, r.Y+2, r.X, r.Y+r.H-2, 1.1, ink, seed+5)
							}
						})
					default:
						cell.BorderWidth(0, 0, 1, 1).BorderColor(t.Border)
					}
					if row == 0 {
						cell.Bold()
						if !md.sketch {
							cell.Background(t.Surface)
						}
					}
					cell.Children(func() {
						if editing && ci == target && col < len(cells) {
							p := preview.tableEditor(positions[ci])
							// Alignment and header weight are inherited by the input.
							ui.Box(c).FillWidth().TextAlign(tableAlignment(blk.table.align[col])).Children(func() {
								a.activeText(c, t, blk, p, nil)
							})
						} else {
							p := liveProjection{text: " "}
							if col < len(cells) {
								p = preview.cells[row][col]
							}
							p.content(c, t, md).FillWidth().FontSize(md.codeSize).TextAlign(tableAlignment(blk.table.align[col]))
						}
					})
					cell.OnClick(func() {
						a.liveClickHandled = true
						a.focusTableCell(blockIndex, row, col)
					})
				}
			})
		})
	})
}

func tableAlignment(align int) ui.Align {
	switch align {
	case 1:
		return ui.End
	case 2:
		return ui.Center
	default:
		return ui.Start
	}
}

func (a *app) focusTableCell(block, row, col int) {
	a.focusBlock(block, 0, 0)
	b := a.mdBlocks()[a.mdActive]
	if a.materializeMissingTableCell(b, row, col) {
		return
	}
	cells := a.previewTable(b).positions
	index := col
	for i := 0; i < row; i++ {
		index += len(b.table.rows[i])
	}
	if index >= len(cells) || col >= len(b.table.rows[row]) {
		return
	}
	cell := cells[index]
	a.selSyncStart, a.selSyncEnd = cell.end, cell.end
	a.liveFocus = 1
}
