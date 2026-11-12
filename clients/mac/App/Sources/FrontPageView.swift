import AppKit
import SwiftUI
import EarnestKit

/// The newspaper front page: a dated masthead, the server's lead story, then
/// its sections in the order the server sent them. The client never ranks or
/// reorders stories; it only renders them.
@MainActor struct FrontPageView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel

    var body: some View {
        VStack(spacing: 0) {
            if !model.briefs.isEmpty {
                BriefsView(briefs: model.briefs, names: model.principalNames)
                Divider()
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    masthead
                    if let page = window.frontPage {
                        sections(page)
                    } else if window.frontPageLoading {
                        skeleton
                    } else if let error = window.frontPageError {
                        message("Front page did not load", error, action: "Try again") {
                            Task { await window.refreshFrontPage(force: true) }
                        }
                    } else {
                        message("No stories in this window",
                                "Nothing matched the \(window.frontPagePeriod == .weekly ? "past 7 days" : "past 24 hours").") {}
                    }
                    notices
                }
            }
        }
        .onChange(of: window.frontPagePeriod) { _, _ in
            Task { await window.refreshFrontPage(force: true) }
        }
    }

    // MARK: - Masthead

    private var masthead: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Front page")
                        .font(.title2.bold())
                        .accessibilityAddTraits(.isHeader)
                    Text(mastheadDate)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 12)
                VStack(alignment: .trailing, spacing: 2) {
                    Text(updated)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    HStack(spacing: 8) {
                        if window.frontPageLoading && window.frontPage != nil {
                            ProgressView().controlSize(.mini)
                        }
                        Button("Refresh") { Task { await window.refreshFrontPage(force: true) } }
                            .buttonStyle(.borderless)
                            .disabled(window.frontPageLoading)
                    }
                }
            }
            tabs
            if window.frontPageAvailable != nil {
                banner("New coverage available", action: "Refresh") { window.applyAvailableFrontPage() }
            }
            if let error = window.frontPageRefreshError {
                banner("Could not refresh the front page. Showing the last update.", action: "Try again") {
                    Task { await window.refreshFrontPage(force: true) }
                }
            }
        }
        .padding(16)
        .background(.bar)
    }

    private var mastheadDate: String {
        let page = window.frontPage
        let text = page?.selectedAt ?? page?.windowEnd ?? ""
        guard let date = DateFormatting.parse(text) else { return "" }
        return date.formatted(date: .long, time: .omitted)
    }

    private var updated: String {
        guard let updated = window.frontPageUpdated else { return "" }
        let relative = updated.formatted(.relative(presentation: .named))
        return model.connectionNote == nil ? "Updated \(relative)" : "Updated \(relative) · Offline"
    }

    private var tabs: some View {
        HStack(spacing: 8) {
            ForEach(FrontPagePeriod.allCases) { period in
                let current = window.frontPagePeriod == period
                Button {
                    window.frontPagePeriod = period
                } label: {
                    VStack(spacing: 1) {
                        Text(period.title)
                            .font(.subheadline.weight(current ? .semibold : .regular))
                        Text(period.windowLabel)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .frame(maxWidth: .infinity)
                    .background(current ? Color.accentColor.opacity(0.15) : Color.secondary.opacity(0.06))
                    .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(period.title), \(period.windowLabel)")
                .accessibilityAddTraits(current ? [.isSelected, .isButton] : .isButton)
            }
        }
    }

    private func banner(_ text: String, action: String, run: @escaping () -> Void) -> some View {
        HStack(spacing: 8) {
            Text(text).font(.callout)
            Spacer(minLength: 8)
            Button(action) { run() }
                .buttonStyle(.borderless)
                .font(.callout.weight(.semibold))
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Tone.orange.color.opacity(0.12))
        .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    // MARK: - Sections and stories

    private func sections(_ page: FrontPage) -> some View {
        let sections = page.sections.filter { !$0.stories.isEmpty }
        let leadID = sections.first?.stories.first?.id
        return VStack(alignment: .leading, spacing: 24) {
            if sections.isEmpty {
                message("No stories in this window",
                        "Nothing matched the \(window.frontPagePeriod == .weekly ? "past 7 days" : "past 24 hours").") {}
            } else {
                ForEach(Array(sections.enumerated()), id: \.offset) { _, section in
                    VStack(alignment: .leading, spacing: 12) {
                        Text(section.name)
                            .font(.headline)
                            .foregroundStyle(.secondary)
                            .accessibilityAddTraits(.isHeader)
                        if section.stories.first?.id == leadID {
                            if let lead = section.stories.first {
                                StoryCell(story: lead, lead: true)
                            }
                            let rest = section.stories.dropFirst()
                            if !rest.isEmpty {
                                LazyVGrid(columns: [GridItem(.adaptive(minimum: 240), spacing: 12)], spacing: 12) {
                                    ForEach(Array(rest)) { story in
                                        StoryCell(story: story, lead: false)
                                    }
                                }
                            }
                        } else {
                            LazyVGrid(columns: [GridItem(.adaptive(minimum: 240), spacing: 12)], spacing: 12) {
                                ForEach(section.stories) { story in
                                    StoryCell(story: story, lead: false)
                                }
                            }
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
        .padding(16)
    }

    private func message(_ title: String, _ detail: String, action: String = "", run: @escaping () -> Void) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.headline)
            if !detail.isEmpty {
                Text(detail).font(.callout).foregroundStyle(.secondary)
            }
            if !action.isEmpty {
                Button(action) { run() }
            }
        }
        .padding(24)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private var skeleton: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(0..<4, id: \.self) { _ in
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(.secondary.opacity(0.08))
                    .frame(height: 96)
            }
        }
        .padding(16)
        .accessibilityLabel("Loading the front page")
    }

    /// The compact session surface: briefs already sit above, and the river's
    /// old end-of-list summary becomes a quiet line under the paper.
    private var notices: some View {
        var parts: [String] = []
        if !model.sessionActions.summary.isEmpty { parts.append(model.sessionActions.summary) }
        if !model.agentChanges.isEmpty {
            let names = Array(Set(model.agentChanges.map(\.principalName).filter { !$0.isEmpty })).sorted()
            let label = names.isEmpty ? "Your agents" : names.joined(separator: ", ")
            let count = model.agentChanges.count
            parts.append("While you were out: \(label) changed \(count) unread entr\(count == 1 ? "y" : "ies"). Review them in Activity.")
        }
        return Group {
            if !parts.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(parts, id: \.self) { part in
                        Text(part).font(.caption).foregroundStyle(.secondary)
                    }
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

/// One story. The headline opens the representative; coverage expands to the
/// other originals. Rendering, switching windows and expanding coverage never
/// write a read mark.
private struct StoryCell: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    let story: FrontPageStory
    let lead: Bool
    @State private var expanded = false
    @State private var cited = false

    private var coverage: [FrontPageCoverage] { window.visibleCoverage(story) }
    private var representative: FrontPageCoverage? { window.representative(story) }
    private var hasOriginal: Bool { representative?.entry.id == story.entry.id }
    private var visibleSummary: FrontPageSummary? {
        let live = Set(coverage.map(\.entry.id))
        guard let summary = story.summary,
              summary.claims.allSatisfy({ $0.citations.allSatisfy { live.contains($0.entryID) } }) else { return nil }
        return summary
    }
    private var others: [FrontPageCoverage] {
        guard let representative else { return [] }
        return coverage.filter { $0.entry.id != representative.entry.id }
    }

    var body: some View {
        if representative != nil {
        VStack(alignment: .leading, spacing: 8) {
            if hasOriginal, let imageURL = story.imageURL, !imageURL.isEmpty {
                FrontImage(path: imageURL)
                    .id("\(model.session):\(imageURL)")
            }
            if let representative {
                Button {
                    window.selectFromList(representative.entry.id)
                } label: {
                    Text(representative.entry.title.isEmpty ? "Untitled" : representative.entry.title)
                        .font(lead ? .title3.bold() : .headline)
                        .multilineTextAlignment(.leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .buttonStyle(.plain)
                .accessibilityHint("Opens the article")
            }
            meta
            summaryOrExcerpt
            coverageLinks
        }
        .padding(lead ? 14 : 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(lead ? Color.secondary.opacity(0.06) : Color.clear)
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        .accessibilityElement(children: .contain)
        }
    }

    private var meta: some View {
        HStack(spacing: 5) {
            if let representative {
                Text(representative.sourceTitle)
            }
            Text("·")
            Text(relative(hasOriginal ? story.publishedAt : representative?.entry.publishedAt ?? representative?.entry.firstSeenAt ?? ""))
            let feedCount = Set(coverage.map(\.entry.feedID)).count
            if feedCount > 1 {
                Text("· Covered by \(feedCount) feeds")
            }
            if window.isStoryRead(story) {
                Image(systemName: "checkmark.circle")
                    .font(.caption2)
                Text("Read")
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var summaryOrExcerpt: some View {
        if let summary = visibleSummary {
            VStack(alignment: .leading, spacing: 4) {
                ForEach(summary.claims, id: \.text) { claim in
                    Text(claim.text)
                        .font(lead ? .body : .callout)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Text("Summary of linked coverage · \(display(summary.generatedAt))")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                if let notice = summary.excerptNotice, !notice.isEmpty {
                    Text(notice)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                if !passages.isEmpty {
                    Button(cited ? "Hide citations" : "Citations") { cited.toggle() }
                        .buttonStyle(.borderless)
                        .font(.caption)
                    if cited {
                        VStack(alignment: .leading, spacing: 4) {
                            ForEach(passages) { passage in
                                citationPassage(passage)
                            }
                        }
                        .padding(.leading, 8)
                    }
                }
            }
        } else if hasOriginal, !story.excerpt.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text(story.excerpt)
                    .font(lead ? .body : .callout)
                    .fixedSize(horizontal: false, vertical: true)
                Text("Excerpt from \(story.sourceTitle)")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        } else {
            Text("No summary or excerpt is available.")
                .font(.callout)
                .foregroundStyle(.secondary)
        }
    }

    private func citationPassage(_ passage: Passage) -> some View {
        let target = coverage.first { $0.entry.id == passage.entryID }
        return Group {
            if let target {
                Button {
                    window.selectFromList(target.entry.id)
                } label: {
                    passageLabel(passage)
                }
                .buttonStyle(.borderless)
            } else {
                passageLabel(passage)
            }
        }
    }

    private func passageLabel(_ passage: Passage) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(passage.verified ? "Verified citation" : "Citation not found in the article")
                .font(.caption2)
                .foregroundStyle(.secondary)
            Text(passage.text)
                .font(.caption)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 2)
    }

    @ViewBuilder private var coverageLinks: some View {
        if !others.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                Button(expanded ? "Hide other sources" : "Other sources (\(others.count))") {
                    expanded.toggle()
                }
                .buttonStyle(.borderless)
                .font(.caption)
                if expanded {
                    ForEach(others) { item in
                        Button {
                            window.selectFromList(item.entry.id)
                        } label: {
                            Text("\(item.sourceTitle) · \(item.entry.title.isEmpty ? "Untitled" : item.entry.title)")
                                .font(.caption)
                                .multilineTextAlignment(.leading)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .buttonStyle(.borderless)
                    }
                }
            }
        }
    }

    private var passages: [Passage] {
        guard let summary = visibleSummary else { return [] }
        var result: [Passage] = []
        for claim in summary.claims {
            for citation in claim.citations {
                for (index, text) in citation.passages.enumerated() {
                    result.append(Passage(id: "\(citation.entryID):\(index)",
                                          entryID: citation.entryID, text: text, verified: citation.verified))
                }
            }
        }
        return result
    }

    private func relative(_ text: String) -> String {
        guard let date = DateFormatting.parse(text) else { return "" }
        return DateFormatting.relative.localizedString(for: date, relativeTo: Date())
    }

    private func display(_ text: String) -> String {
        guard let date = DateFormatting.parse(text) else { return "" }
        return date.formatted(date: .abbreviated, time: .omitted)
    }
}

private struct Passage: Identifiable {
    let id: String
    let entryID: Int64
    let text: String
    let verified: Bool
}

/// The server's stored feature image. Nothing draws until it loads, and an
/// absent or broken image leaves the story text-only.
private struct FrontImage: View {
    let path: String
    @EnvironmentObject private var model: AppModel
    @State private var image: NSImage?
    @State private var failed = false

    var body: some View {
        ZStack {
            if let image {
                Image(nsImage: image)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
            } else if !failed {
                Rectangle().fill(.secondary.opacity(0.08))
                ProgressView().controlSize(.small)
            }
        }
        .frame(maxWidth: .infinity)
        .frame(height: failed ? 0 : 200)
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        .accessibilityHidden(true)
        .task(id: path) {
            let loaded = await model.media.image(for: path)
            image = loaded
            failed = loaded == nil
        }
    }
}
