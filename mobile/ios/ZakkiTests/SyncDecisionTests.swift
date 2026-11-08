import XCTest
@testable import Zakki

// D13 判定表（§2.2）：四情况逐条 + 基线缺失/远端缺失变体 + 写保护拦截。
// 纯函数判定，注入 sha256，时钟无关。
final class SyncDecisionTests: XCTestCase {
    private let sha = SyncHash.sha256Hex
    private let baseContent = ##"{"version":3,"active":0,"tabs":[{"id":1,"name":"A","text":"hello","color":"#e5484d"}]}"##
    private let localEdit = ##"{"version":3,"active":0,"tabs":[{"id":1,"name":"A","text":"hello local","color":"#e5484d"}]}"##
    private let remoteEdit = ##"{"version":3,"active":0,"tabs":[{"id":1,"name":"A","text":"hello remote","color":"#e5484d"}]}"##

    private func input(baseline: SyncBaseline?, local: String, writable: Bool = true,
                       remote: RemoteFile) -> ConflictDecisionInput {
        ConflictDecisionInput(baseline: baseline, localRaw: local, localWritable: writable, remote: remote)
    }

    private func baseline(content: String, version: String) -> SyncBaseline {
        SyncBaseline(backendKind: "github", contentSHA256: sha(content), remoteVersion: version)
    }

    // 情况 1：本地等于基线、远端已变 → 采用远端。
    func test_case1_localUnchangedRemoteChanged_adoptsRemote() {
        let d = ConflictDecision.evaluate(
            input(baseline: baseline(content: baseContent, version: "v1"), local: baseContent,
                  remote: RemoteFile(exists: true, content: remoteEdit, version: "v2")),
            sha256Hex: sha)
        XCTAssertEqual(d, .adoptRemote)
    }

    // 情况 2：本地已变、远端等于基线 → 上传。
    func test_case2_localChangedRemoteUnchanged_pushesLocal() {
        let d = ConflictDecision.evaluate(
            input(baseline: baseline(content: baseContent, version: "v1"), local: localEdit,
                  remote: RemoteFile(exists: true, content: baseContent, version: "v1")),
            sha256Hex: sha)
        XCTAssertEqual(d, .pushLocal)
    }

    // 情况 3：双方已变但内容相同 → 只更新基线。
    func test_case3_bothChangedSameContent_updatesBaselineOnly() {
        let d = ConflictDecision.evaluate(
            input(baseline: baseline(content: baseContent, version: "v1"), local: localEdit,
                  remote: RemoteFile(exists: true, content: localEdit, version: "v2")),
            sha256Hex: sha)
        XCTAssertEqual(d, .baselineOnly)
    }

    // 情况 4：双方已变且内容不同 → 冲突对话框。
    func test_case4_bothChangedDifferentContent_conflict() {
        let d = ConflictDecision.evaluate(
            input(baseline: baseline(content: baseContent, version: "v1"), local: localEdit,
                  remote: RemoteFile(exists: true, content: remoteEdit, version: "v2")),
            sha256Hex: sha)
        XCTAssertEqual(d, .conflict)
    }

    // 双方都未变 → 无动作。
    func test_nothingChanged_isIdle() {
        let d = ConflictDecision.evaluate(
            input(baseline: baseline(content: baseContent, version: "v1"), local: baseContent,
                  remote: RemoteFile(exists: true, content: baseContent, version: "v1")),
            sha256Hex: sha)
        XCTAssertEqual(d, .none)
    }

    // 首次同步（无基线）且远端已有不同内容：本地与远端都算「已变」且不同 → 冲突，
    // 用户二选一，不默认覆盖任何一方。
    func test_noBaseline_remoteExistsDifferent_conflict() {
        let d = ConflictDecision.evaluate(
            input(baseline: nil, local: localEdit,
                  remote: RemoteFile(exists: true, content: remoteEdit, version: "v2")),
            sha256Hex: sha)
        XCTAssertEqual(d, .conflict)
    }

    // 首次同步且远端内容与本地相同 → 只更新基线。
    func test_noBaseline_sameContent_baselineOnly() {
        let d = ConflictDecision.evaluate(
            input(baseline: nil, local: localEdit,
                  remote: RemoteFile(exists: true, content: localEdit, version: "v9")),
            sha256Hex: sha)
        XCTAssertEqual(d, .baselineOnly)
    }

    // 远端缺失：本地内容永远有（首启种子）→ 上传/回补，绝不清空本地迁就空远端。
    func test_remoteMissing_pushesLocalRestore() {
        let d = ConflictDecision.evaluate(
            input(baseline: baseline(content: baseContent, version: "v1"), local: baseContent,
                  remote: .missing),
            sha256Hex: sha)
        XCTAssertEqual(d, .pushLocal)
    }

    // 远端缺失且无基线（从未上传）→ 首次上传。
    func test_remoteMissingNoBaseline_pushesLocalCreate() {
        let d = ConflictDecision.evaluate(
            input(baseline: nil, local: baseContent, remote: .missing),
            sha256Hex: sha)
        XCTAssertEqual(d, .pushLocal)
    }

    // 写保护（高版本/损坏/超限）：禁止上传与自动覆盖（§2.1），判定让位于拦截。
    func test_writeProtected_blocksBeforeEverything() {
        let d = ConflictDecision.evaluate(
            input(baseline: baseline(content: baseContent, version: "v1"), local: baseContent,
                  writable: false,
                  remote: RemoteFile(exists: true, content: remoteEdit, version: "v2")),
            sha256Hex: sha)
        XCTAssertEqual(d, .blockedWriteProtected)
    }
}
