package notes

// 同步合并（ADR-0004）：本机与远端都相对基线变化时，以基线原文为公共祖先
// 逐标签三方合并；只有同一标签两边都改了才按「较新者胜」。不弹窗、不留档。
// 无法逐标签合并（缺基线原文、任一侧解码失败、结果超标签上限）时退化为
// 整个文件较新者胜。mergeSyncContent / mergeDocuments 是纯函数；syncMergeRound
// 负责不持锁查远端时间、算合并、再加锁写回本地。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"zakki/core/model"
	"zakki/internal/atomicfile"
)

// syncPlan 是持锁本地阶段的结论：要么带出上传任务，要么带出待合并的现场。
type syncPlan struct {
	push  *syncPushRequest
	merge *syncMergeInput
}

// syncMergeInput 是合并所需的三方内容与判定时的本地保存代号（重新加锁后校验）。
type syncMergeInput struct {
	base            syncBaselineV1
	local, remote   []byte
	version         string
	rev0            uint64
	localModifiedAt time.Time
}

// syncMergeRound 处理双方都改的情况（ADR-0004）：不持锁查远端修改时间、算合并
// 结果，再重新加锁写回本地。合并结果与远端不同时返回上传任务。
func (s *Store) syncMergeRound(ctx context.Context, gen uint64, backend SyncBackend, in *syncMergeInput) (*syncPushRequest, error) {
	localNewer := true
	if mt, ok := backend.(SyncModTimer); ok {
		if remoteAt, err := mt.RemoteModTime(ctx); err != nil {
			slog.Warn("查询远端修改时间失败，按本机较新处理", "error", err)
		} else if !remoteAt.IsZero() {
			localNewer = !remoteAt.After(in.localModifiedAt)
		}
	}
	merged, perTab := mergeSyncContent(in.base.Content, in.local, in.remote, localNewer, s.migrate)
	slog.Info("同步合并", "perTab", perTab, "localNewer", localNewer, "merged", contentHashPrefix(contentHash(merged)))

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.gitGate.Lock()
	defer s.gitGate.Unlock()
	s.mu.Lock()
	valid := s.syncValidLocked(gen) && !s.syncHold && !s.saveBlocked && !s.invalid
	s.mu.Unlock()
	if !valid {
		return nil, nil
	}
	switch {
	case bytes.Equal(merged, in.remote):
		// 合并结果就是远端：等同采用远端，不必上传。
		return nil, s.syncAdopt(in.remote, in.version, true, in.rev0, true)
	case !bytes.Equal(merged, in.local):
		if err := s.syncWriteMerged(merged, in.rev0); err != nil {
			return nil, err
		}
	default:
		// 合并结果就是本机：只需上传，不动本地文件。仍需确认合并期间没有新的
		// 本地保存，否则会把旧快照传上去。
		if err := s.syncCheckNoNewSaves(in.rev0); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.syncState.LastMerge = s.now()
		s.mu.Unlock()
	}
	return &syncPushRequest{
		content: merged,
		expect:  in.version,
		basic:   s.baselineUpdated(backend, merged, "", true, true),
	}, nil
}

// syncCheckNoNewSaves 在判定与写入之间又出现新的本地快照时中止本轮（不丢
// 输入），下一轮按新的本地内容重判。调用方持有 writeMu。
func (s *Store) syncCheckNoNewSaves(rev0 uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil || s.saveRevision != rev0 {
		return errors.New("同步期间出现新的本地修改，本轮已跳过，稍后自动重试")
	}
	return nil
}

// syncWriteMerged 把合并结果写入本地并冻结保存、请求界面重载；基线等上传
// 成功后再写。上传失败时本地已是合并结果，下一轮以旧基线重新合并仍然收敛。
func (s *Store) syncWriteMerged(merged []byte, rev0 uint64) error {
	if err := s.verifyDisk(); err != nil {
		return fmt.Errorf("合并内容未能安全落盘：%w", err)
	}
	if err := s.syncCheckNoNewSaves(rev0); err != nil {
		return err
	}
	if err := s.stateWrite(s.dataPath, merged); err != nil && !atomicfile.IsCommitted(err) {
		return fmt.Errorf("写入合并内容失败：%w", err)
	}
	s.mu.Lock()
	s.diskKnown, s.diskExists = true, true
	s.diskHash = sha256.Sum256(merged)
	s.savedContent = append([]byte(nil), merged...)
	s.resetLoadedSaveLocked()
	s.syncHold = true
	now := s.now()
	s.syncState.LastRemoteApply, s.syncState.LastMerge = now, now
	s.mu.Unlock()
	return nil
}

// mergeTab 是参与合并的一条标签：已知字段与该标签的未知字段一起比较、一起去留。
type mergeTab struct {
	tab     Tab
	unknown map[string]json.RawMessage
}

func (a mergeTab) equal(b mergeTab) bool {
	return a.tab == b.tab && rawMapsEqual(a.unknown, b.unknown)
}

// mergeSyncContent 合并三方内容。localNewer 决定「两边都改了同一处」时谁胜出。
// perTab=false 表示未能逐标签合并、已退化为整个文件较新者胜。
func mergeSyncContent(base, local, remote []byte, localNewer bool, opt MigrateOptions) (merged []byte, perTab bool) {
	whole := remote
	if localNewer {
		whole = local
	}
	if base == nil {
		return whole, false
	}
	b, errB := model.Decode(base, opt)
	l, errL := model.Decode(local, opt)
	r, errR := model.Decode(remote, opt)
	if errB != nil || errL != nil || errR != nil {
		return whole, false
	}
	out, ok := mergeDocuments(b, l, r, localNewer, opt.MaxTabCount)
	if !ok {
		return whole, false
	}
	raw, err := model.Encode(out)
	if err != nil {
		return whole, false
	}
	// 合并结果必须能被正常载入，否则宁可整体取较新者也不写出坏文件。
	if _, err := model.Decode(raw, opt); err != nil {
		return whole, false
	}
	return raw, true
}

// mergeDocuments 是合并的核心：标签按稳定 ID 三方合并，settings 与顶层未知字段
// 各作为整体三方合并。规则：只有一边改 → 取改的一边；两边都改 → 较新者胜；
// 一边删、另一边改 → 保留改动（不静默丢内容）；两边各自新建了同 ID 标签 →
// 两条都保留，较旧一侧换新 ID。标签顺序以较新一侧为准，另一侧独有的接在末尾。
func mergeDocuments(b, l, r model.Document, localNewer bool, maxTabs int) (model.Document, bool) {
	bt, lt, rt := tabsByID(b), tabsByID(l), tabsByID(r)
	newer, older := l, r
	nt, ot := lt, rt
	if !localNewer {
		newer, older = r, l
		nt, ot = rt, lt
	}
	nextID := maxTabID(b, l, r) + 1

	kept := map[uint64]mergeTab{}
	var renamed []mergeTab // 较旧一侧新建且与较新一侧擞 ID 的标签，换号后接在末尾
	for _, id := range unionIDs(b, l, r) {
		base, inB := bt[id]
		n, inN := nt[id]
		o, inO := ot[id]
		switch {
		case inB && inN && inO:
			switch {
			case n.equal(base):
				kept[id] = o
			default:
				// 较新一侧改了：无论较旧一侧是否也改，都以较新一侧为准。
				kept[id] = n
			}
		case inB && inN: // 较旧一侧删了
			if !n.equal(base) {
				kept[id] = n
			}
		case inB && inO: // 较新一侧删了
			if !o.equal(base) {
				kept[id] = o
			}
		case inN && inO: // 两边各自新建
			kept[id] = n
			if !n.equal(o) {
				o.tab.ID = nextID
				nextID++
				renamed = append(renamed, o)
			}
		case inN:
			kept[id] = n
		case inO:
			kept[id] = o
		}
	}

	var tabs []Tab
	tabUnknown := map[uint64]map[string]json.RawMessage{}
	add := func(t mergeTab) {
		tabs = append(tabs, t.tab)
		if len(t.unknown) > 0 {
			tabUnknown[t.tab.ID] = t.unknown
		}
	}
	placed := map[uint64]bool{}
	for _, side := range []model.Document{newer, older} {
		for _, tb := range side.File.Tabs {
			if t, ok := kept[tb.ID]; ok && !placed[tb.ID] {
				placed[tb.ID] = true
				add(t)
			}
		}
	}
	for _, t := range renamed {
		add(t)
	}
	if len(tabs) == 0 || len(tabs) > maxTabs {
		return model.Document{}, false
	}

	f := newer.File
	f.Tabs, f.Active = tabs, 0
	settingsSide := newer
	switch {
	case settingsEqual(l, b):
		settingsSide = r
	case settingsEqual(r, b):
		settingsSide = l
	}
	f.Settings = settingsSide.File.Settings
	var topUnknown map[string]json.RawMessage
	switch {
	case rawMapsEqual(l.Unknown, b.Unknown):
		topUnknown = r.Unknown
	case rawMapsEqual(r.Unknown, b.Unknown):
		topUnknown = l.Unknown
	default:
		topUnknown = newer.Unknown
	}
	// 当前标签跟本机走：合并在这台设备上发生，不该把用户正在看的标签切走。
	if act := activeTabID(l); act != 0 {
		if i := slices.IndexFunc(tabs, func(t Tab) bool { return t.ID == act }); i >= 0 {
			f.Active = i
		}
	}
	return model.Document{File: f, Unknown: topUnknown, SettingsUnknown: settingsSide.SettingsUnknown, TabUnknown: tabUnknown}, true
}

func tabsByID(d model.Document) map[uint64]mergeTab {
	m := make(map[uint64]mergeTab, len(d.File.Tabs))
	for _, tb := range d.File.Tabs {
		m[tb.ID] = mergeTab{tab: tb, unknown: d.TabUnknown[tb.ID]}
	}
	return m
}

// unionIDs 按首次出现顺序列出三方全部标签 ID（只用于遍历，输出顺序另算）。
func unionIDs(docs ...model.Document) []uint64 {
	seen := map[uint64]bool{}
	var ids []uint64
	for _, d := range docs {
		for _, tb := range d.File.Tabs {
			if !seen[tb.ID] {
				seen[tb.ID] = true
				ids = append(ids, tb.ID)
			}
		}
	}
	return ids
}

func maxTabID(docs ...model.Document) uint64 {
	var m uint64
	for _, d := range docs {
		for _, tb := range d.File.Tabs {
			m = max(m, tb.ID)
		}
	}
	return m
}

func activeTabID(d model.Document) uint64 {
	if d.File.Active < 0 || d.File.Active >= len(d.File.Tabs) {
		return 0
	}
	return d.File.Tabs[d.File.Active].ID
}

func settingsEqual(a, b model.Document) bool {
	return a.File.Settings == b.File.Settings && rawMapsEqual(a.SettingsUnknown, b.SettingsUnknown)
}

// rawMapsEqual 把 nil 与空 map 视为相同；RawMessage 按字节比较（同一编码管线写出）。
func rawMapsEqual(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !bytes.Equal(va, vb) {
			return false
		}
	}
	return true
}
