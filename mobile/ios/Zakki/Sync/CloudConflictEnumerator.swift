import Foundation

// MARK: - 云盘冲突副本枚举（双通道：命名扫描 + NSFileVersion）

// 通道 1：文件夹内按命名扫描（<base> (N).ext 与 conflicted copy，数值排序见 ConflictScanner）；
// 仅文件夹授权可用——仅文件授权无法列父目录。
// 通道 2：NSFileVersion.unresolvedConflictVersionsOfItem（云盘自身版本库的冲突标记）；
// getNonlocalVersions 对非 iCloud 文件报 NSFileProviderError（探针 §3），按空集容错。
// 副本内容经 PlaceholderReader 读取（副本同样可能是占位文件）。
enum CloudConflictEnumerator {
    struct Copy: Identifiable, Equatable {
        enum Source: String {
            case namedCopy // 命名扫描
            case fileVersion // NSFileVersion
        }

        var fileName: String // 展示名（namedCopy=文件名；fileVersion=localizedName）
        var modifiedAtMs: Int64
        var content: String
        var version: String // 作为对话框候选的远端版本号（仅展示与追溯用；CAS 以主文件版本为基准）
        var source: Source
        var id: String { "\(source.rawValue)#\(fileName)#\(version)" }
    }

    enum Outcome: Equatable {
        case success([Copy])
        case failure(SyncError)
    }

    // 枚举双通道副本。任何单通道失败不影响另一通道结果（内容读取失败的条目跳过）。
    // 每副本读取用独立短超时：占位副本慢下载不阻塞整个列表（真机项，模拟器走本地快速路径）。
    static func enumerate(bookmarkData: Data,
                          copyReadTimeout: TimeInterval = 10,
                          pollInterval: TimeInterval = 0.5) async -> Outcome {
        let root: URL
        let scope: CloudGrantScope
        do {
            let resolved = try CloudFolderAccess.resolve(bookmarkData: bookmarkData)
            root = resolved.rootURL
            scope = resolved.scope
        } catch let error as CloudFolderError {
            return .failure(CloudFolderBackend.map(error))
        }
        var access = CloudFolderAccess(rootURL: root, scope: scope)
        do {
            try access.startAccessing()
        } catch let error as CloudFolderError {
            return .failure(CloudFolderBackend.map(error))
        }
        defer { access.stopAccessing() }
        let mainURL = access.mainURL
        var copies: [Copy] = []
        // 通道 1：命名扫描（文件夹授权）。
        if scope == .folder, FileManager.default.fileExists(atPath: root.path) {
            let scan = ConflictScanner.scan(target: mainURL, in: root)
            for url in scan.merged {
                if let copy = await readCopy(at: url, source: .namedCopy,
                                             timeout: copyReadTimeout, pollInterval: pollInterval) {
                    copies.append(copy)
                }
            }
        }
        // 通道 2：NSFileVersion 冲突版本。
        copies.append(contentsOf: fileVersionCopies(of: mainURL))
        return .success(copies)
    }

    // 读取单个命名副本；读取失败（占位超时等）跳过该条，不阻塞整体枚举。
    private static func readCopy(at url: URL, source: Copy.Source,
                                 timeout: TimeInterval,
                                 pollInterval: TimeInterval) async -> Copy? {
        guard FileManager.default.fileExists(atPath: url.path) else { return nil }
        let data: Data
        do {
            data = try await PlaceholderReader.read(url: url, timeout: timeout, pollInterval: pollInterval)
        } catch {
            return nil
        }
        guard let content = String(data: data, encoding: .utf8) else { return nil }
        return Copy(fileName: url.lastPathComponent,
                    modifiedAtMs: CloudFolderAccess.modificationMs(of: url),
                    content: content,
                    version: CloudFolderAccess.version(of: url) ?? url.lastPathComponent,
                    source: source)
    }

    // 通道 2：unresolvedConflictVersions 内容经 replaceItem 提取到临时文件再读
    //（NSFileVersion 不直接暴露内容数据；与「恢复」用同一 API 形态）。
    private static func fileVersionCopies(of mainURL: URL) -> [Copy] {
        let current = NSFileVersion.currentVersionOfItem(at: mainURL)
        // 非 iCloud 文件会抛 NSFileProviderError（探针实测）：按无冲突容错。
        guard let versions = NSFileVersion.unresolvedConflictVersionsOfItem(at: mainURL), !versions.isEmpty
        else { return [] }
        var copies: [Copy] = []
        let scratchDir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-cloud-versions-\(UUID().uuidString)", isDirectory: true)
        try? FileManager.default.createDirectory(at: scratchDir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: scratchDir) }
        for (index, version) in versions.enumerated() {
            // 当前版本本身不在恢复候选里（跳过与 currentVersion 同名同刻的条目）。
            if let current,
               version.modificationDate == current.modificationDate,
               version.localizedName == current.localizedName {
                continue
            }
            let scratch = scratchDir.appendingPathComponent("v\(index)", isDirectory: false)
            do {
                try Data().write(to: scratch)
                _ = try version.replaceItem(at: scratch, options: [])
                guard let data = try? Data(contentsOf: scratch),
                      let content = String(data: data, encoding: .utf8) else { continue }
                let ms = version.modificationDate.map { Int64($0.timeIntervalSince1970 * 1000) } ?? 0
                copies.append(Copy(fileName: version.localizedName ?? String(format: "v%d", index + 1),
                                   modifiedAtMs: ms,
                                   content: content,
                                   version: "version-\(ms)-\(index)",
                                   source: .fileVersion))
            } catch {
                continue // 单版本内容不可读（如未下载）：跳过，不阻塞其余条目
            }
        }
        return copies
    }

    // 采用候选并完成同步后，尽力清除版本库冲突标记（保留版本文件本身，不删数据；
    // 失败静默——残留条目会在下次枚举时如实再现）。
    static func resolveVersionMarkers(bookmarkData: Data) {
        guard let (root, scope) = try? CloudFolderAccess.resolve(bookmarkData: bookmarkData) else { return }
        var access = CloudFolderAccess(rootURL: root, scope: scope)
        guard (try? access.startAccessing()) != nil else { return }
        defer { access.stopAccessing() }
        let versions = NSFileVersion.unresolvedConflictVersionsOfItem(at: access.mainURL) ?? []
        for version in versions {
            // byDeletingCurrentVersion=false：只清冲突标记，不动任何文件内容。
            try? version.resolve(byDeletingCurrentVersion: false)
        }
    }
}
