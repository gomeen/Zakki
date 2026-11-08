import Foundation
import CryptoKit

// MARK: - 后端抽象的值类型（§2.2：双后端任一时刻只激活一个）

// 后端种类；后端 B（云盘文件夹，#53）占位，实现同一 SyncBackend 契约接入。
enum SyncBackendKind: String, Codable, CaseIterable {
    case github // A. GitHub Contents API（sha CAS）
    case cloudFolder // B. 云盘文件夹（#53，暂未实现，UI 灰显占位）
}

// 远端 zakki.json 快照：内容与后端版本号（后端 A 为 blob sha，B 为文件身份+修改时间，留位）。
struct RemoteFile: Equatable, Sendable {
    var exists: Bool
    var content: String?
    var version: String?

    static let missing = RemoteFile(exists: false, content: nil, version: nil)
}

// 同步基线（D13）：上次同步成功的内容 sha256 + 后端版本号。
// 只存设备本地独立文件；禁止写进 zakki.json、偏好文件或 Git（§2.2）。
struct SyncBaseline: Codable, Equatable {
    var backendKind: String
    var contentSHA256: String
    var remoteVersion: String?
}

// MARK: - D13 判定表（纯函数，时钟无关）

// 判定输入：基线、flush 后的本地字节与可写性、远端快照。
struct ConflictDecisionInput {
    var baseline: SyncBaseline?
    var localRaw: String
    var localWritable: Bool
    var remote: RemoteFile
}

// 判定结果；与 §2.2 表格一一对应（「已变」指与本地基线比较，冲突按整个文件处理）。
enum SyncDecision: Equatable {
    case none // 本地与远端都未变
    case adoptRemote // 本地等于基线、远端已变（无未保存修改，flush 已保证）
    case pushLocal // 本地已变、远端等于基线（含远端缺失回补）
    case baselineOnly // 双方已变但内容相同，只更新基线
    case conflict // 双方已变且内容不同：弹对话框二选一
    case blockedWriteProtected // 写保护：禁止上传与自动覆盖（§2.1）
}

enum ConflictDecision {
    // sha256Hex 注入便于测试；生产用 CryptoKit 实现。
    static func evaluate(_ input: ConflictDecisionInput, sha256Hex: (String) -> String) -> SyncDecision {
        // §2.1：写保护文件保留原始字节，禁止同步上传和自动覆盖——整轮停在这里。
        guard input.localWritable else { return .blockedWriteProtected }
        let localChanged = localHasChanged(input, sha256Hex: sha256Hex)
        let remoteChanged = remoteHasChanged(input)
        // 远端缺失：文件从未上传或被外部删除。本地永远有内容（首启种子），
        // 按「上传/回补」处理是破坏性最小的路径——绝不能把本地清空去迁就空远端。
        guard input.remote.exists, let remoteContent = input.remote.content else {
            return .pushLocal
        }
        switch (localChanged, remoteChanged) {
        case (false, false):
            return .none
        case (false, true):
            return .adoptRemote
        case (true, false):
            return .pushLocal
        case (true, true):
            // 「与本地内容相同」按字节比较（sha256）；相同则只更新基线。
            return sha256Hex(input.localRaw) == sha256Hex(remoteContent) ? .baselineOnly : .conflict
        }
    }

    // 本地「已变」：无基线（从未同步）或当前内容 sha256 ≠ 基线内容 sha256。
    private static func localHasChanged(_ input: ConflictDecisionInput, sha256Hex: (String) -> String) -> Bool {
        guard let baseline = input.baseline else { return true }
        return sha256Hex(input.localRaw) != baseline.contentSHA256
    }

    // 远端「已变」：无基线或远端版本号 ≠ 基线记录的后端版本号。
    private static func remoteHasChanged(_ input: ConflictDecisionInput) -> Bool {
        guard let baseline = input.baseline else { return true }
        guard input.remote.exists else { return baseline.remoteVersion != nil }
        return input.remote.version != baseline.remoteVersion
    }
}

// MARK: - 冲突候选（对话框呈现与裁决的载体）

// 判定为 conflict 时暂存的双方内容；用户二选一后按此执行，CAS 以 casBaseVersion 为基准
// ——若期间远端又前进，push 会 CAS 失败并回到重拉重判定（D12）。
// casBaseVersion：写回/基线记录所用的远端主文件版本号。后端 A 与 remoteVersion 相同
//（即主文件 blob sha）；后端 B 采用云盘副本候选时是主文件当前版本号，而非副本自身版本
//（落盘目标是主文件，基线也必须记录主文件的下一轮版本号）。
struct ConflictCandidate: Identifiable, Equatable {
    var id: String { remoteVersion }
    var remoteContent: String
    var remoteVersion: String
    var localRaw: String
    var casBaseVersion: String?

    // 后端 A 路径：候选即主文件，CAS/基线基准与候选版本一致。
    init(remoteContent: String, remoteVersion: String, localRaw: String,
         casBaseVersion: String? = nil) {
        self.remoteContent = remoteContent
        self.remoteVersion = remoteVersion
        self.localRaw = localRaw
        self.casBaseVersion = casBaseVersion ?? remoteVersion
    }
}

// MARK: - 轮次结果与持久化状态

// 一轮同步的摘要（设置页状态展示用；详情进验证报告，不含任何凭据）。
enum SyncOutcome: Equatable, Codable {
    case idle // 无变化
    case adoptedRemote // 已采用远端
    case pushedLocal // 已上传本地
    case baselineUpdated // 内容相同，仅更新基线
    case conflictPending // 冲突待用户选择
    case blockedWriteProtected
    case notConnected
    case failed(String) // 失败原因（错误码，不夹带 token）
}

struct SyncSummary: Equatable, Codable {
    var outcome: SyncOutcome
    var atMs: Int64
}

// 设备本地同步状态文件（Application Support/zakki-sync-state.json）：
// 基线 + 待上传留档清单 + 最近一次摘要。整份文件不参与同步、不上传（D13）。
// Encodable 走合成；Decodable 自定义 init（见扩展），避免默认值阻断合成。
struct SyncPersistedState: Encodable, Equatable {
    var baseline: SyncBaseline?
    // 已写入本地 conflicts/ 但尚未确认上传成功的留档文件名；成功后移出。
    var pendingArchives: [String]
    var lastSummary: SyncSummary?

    init(baseline: SyncBaseline?, pendingArchives: [String] = [], lastSummary: SyncSummary? = nil) {
        self.baseline = baseline
        self.pendingArchives = pendingArchives
        self.lastSummary = lastSummary
    }
}

extension SyncPersistedState: Decodable {
    // 旧文件缺 pendingArchives 键时容忍（decodeIfPresent）；字段只增不减。
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        baseline = try c.decodeIfPresent(SyncBaseline.self, forKey: .baseline)
        pendingArchives = try c.decodeIfPresent([String].self, forKey: .pendingArchives) ?? []
        lastSummary = try c.decodeIfPresent(SyncSummary.self, forKey: .lastSummary)
    }
}

// MARK: - 错误（客户端按错误码处理，不匹配文案；文案在 Strings）

enum SyncError: Error, Equatable {
    case casConflict // PUT 409/422 或写前版本比对失败：远端已前进，需重新 pull 重判定（不盲重试）
    case needsReauth // 401 且刷新失败：token 过期/被撤销，需重新 Device Flow
    case notConfigured // 缺 client_id / owner/repo / token
    case api(String) // 其他 API 错误（`code: 说明` 形式）
    case deviceFlow(String) // Device Flow 失败（expired/denied 等）
    // 后端 B（#53）：书签失效（目标被删/移动）→ 引导重新选位置；
    // 授权被撤销（系统设置）→ 同样走重选；一切 startAccessing/读写按可失败设计（探针 §5）。
    case bookmarkInvalid
    case accessDenied
    // 后端 B 仅文件授权：写不进 zakki.json 同目录的 conflicts/，留档保留设备本地即可。
    case archiveNotSupported
}

// MARK: - 工具

enum SyncHash {
    // 基线内容指纹（CryptoKit；基线只存十六进制摘要，不存内容）。
    static func sha256Hex(_ s: String) -> String {
        SHA256.hash(data: Data(s.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}
