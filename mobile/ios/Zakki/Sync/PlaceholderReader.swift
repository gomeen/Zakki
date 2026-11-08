import Foundation

// MARK: - 占位文件安全读取（探针 §2 的 PlaceholderReader 异步化移植）

// 仅云端占位文件不是可读文件（R8）：检测下载状态 → startDownloadingUbiquitousItem
// → 轮询到 .current → NSFileCoordinator(.withoutChanges) 读。本地文件走快速路径直读，
// 检测用 `?? false` 兜底，不会把本地文件误判为占位（探针单测已验证）。
// 探针版的阻塞轮询改为 async 轮询，不占主线程；sleep/超时可注入供测试。
enum PlaceholderReader {
    enum Verdict: Equatable {
        case localFile
        case downloaded
        case placeholderNotDownloaded
    }

    struct Diagnosis: Equatable {
        let isUbiquitous: Bool
        let statusRaw: String?
        let verdict: Verdict
    }

    // 诊断下载状态；非 iCloud 文件两个键都缺失（nil），verdict 落 localFile。
    static func diagnose(url: URL) -> Diagnosis {
        let values = try? url.resourceValues(forKeys: [
            .isUbiquitousItemKey,
            .ubiquitousItemDownloadingStatusKey,
        ])
        let isUbiquitous = values?.isUbiquitousItem ?? false
        let status = values?.ubiquitousItemDownloadingStatus
        let verdict: Verdict
        if !isUbiquitous {
            verdict = .localFile
        } else if status == .notDownloaded {
            verdict = .placeholderNotDownloaded
        } else {
            verdict = .downloaded
        }
        return Diagnosis(isUbiquitous: isUbiquitous,
                         statusRaw: status.map { String(describing: $0) },
                         verdict: verdict)
    }

    // 安全读取：返回全文数据。占位分支失败按 downloadTimeout / readFailed 报告。
    // startDownloadingUbiquitousItem 会抛错（探针实测），必须 catch。
    static func read(url: URL,
                     timeout: TimeInterval = 30,
                     pollInterval: TimeInterval = 0.5,
                     sleep: (TimeInterval) async throws -> Void = {
                         try await Task.sleep(nanoseconds: UInt64($0 * 1_000_000_000))
                     }) async throws -> Data {
        let diagnosis = diagnose(url: url)
        if diagnosis.verdict == .placeholderNotDownloaded {
            do {
                try FileManager.default.startDownloadingUbiquitousItem(at: url)
            } catch {
                throw CloudFolderError.readFailed(code: "start-download-\((error as NSError).code)")
            }
            let deadline = Date().addingTimeInterval(timeout)
            while Date() < deadline {
                let status = try? url.resourceValues(forKeys: [.ubiquitousItemDownloadingStatusKey])
                    .ubiquitousItemDownloadingStatus
                if status == .current { break }
                try await sleep(pollInterval)
            }
            let status = try? url.resourceValues(forKeys: [.ubiquitousItemDownloadingStatusKey])
                .ubiquitousItemDownloadingStatus
            if status != .current {
                throw CloudFolderError.downloadTimeout
            }
        }
        // NSFileCoordinator 协调读（iCloud 项目并发安全；.withoutChanges 避免触碰修改时间）。
        var data: Data?
        var coordError: NSError?
        NSFileCoordinator().coordinate(readingItemAt: url, options: [.withoutChanges], error: &coordError) { u in
            do {
                data = try Data(contentsOf: u)
            } catch {
                coordError = error as NSError
            }
        }
        if let coordError {
            throw CloudFolderError.mapIOError(coordError, reading: true)
        }
        guard let data else {
            throw CloudFolderError.readFailed(code: "coordinated-empty")
        }
        return data
    }
}
