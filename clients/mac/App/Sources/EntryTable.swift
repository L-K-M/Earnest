import Foundation
import AppKit
import SwiftUI

private enum TableItem: Equatable {
    case header(String)
    case entry(EntryRow)

    var entryID: Int64? {
        if case .entry(let row) = self { return row.id }
        return nil
    }
}

struct EntryTable: NSViewRepresentable {
    let entries: [EntryRow]
    let groups: [EditionGroup]
    @Binding var selection: Int64?

    init(entries: [EntryRow] = [], groups: [EditionGroup] = [], selection: Binding<Int64?>) {
        self.entries = entries
        self.groups = groups
        _selection = selection
    }

    private var items: [TableItem] {
        if groups.isEmpty { return entries.map(TableItem.entry) }
        return groups.flatMap { [.header($0.title)] + $0.rows.map(TableItem.entry) }
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let table = NSTableView()
        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("title"))
        column.title = "Articles"
        table.addTableColumn(column)
        table.columnAutoresizingStyle = .lastColumnOnlyAutoresizingStyle
        table.headerView = nil
        table.delegate = context.coordinator
        table.dataSource = context.coordinator
        table.rowHeight = 44
        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        guard let table = scroll.documentView as? NSTableView else { return }
        let coordinator = context.coordinator
        coordinator.updating = true
        defer { coordinator.updating = false }
        coordinator.parent = self
        let newItems = items
        // Reload only when the rows actually changed: j/k selection updates
        // otherwise rebuilt every visible row view on each key press.
        if coordinator.items != newItems {
            coordinator.items = newItems
            table.reloadData()
        }
        if let selection, let row = coordinator.items.firstIndex(where: { $0.entryID == selection }) {
            table.selectRowIndexes(IndexSet(integer: row), byExtendingSelection: false)
            table.scrollRowToVisible(row)
        } else {
            table.deselectAll(nil)
        }
    }

    final class Coordinator: NSObject, NSTableViewDataSource, NSTableViewDelegate {
        var parent: EntryTable
        fileprivate var items: [TableItem]
        var updating = false

        init(_ parent: EntryTable) { self.parent = parent; items = parent.items }
        func numberOfRows(in tableView: NSTableView) -> Int { items.count }

        func tableView(_ tableView: NSTableView, isGroupRow row: Int) -> Bool { items[row].entryID == nil }
        func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool { items[row].entryID != nil }
        func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat { items[row].entryID == nil ? 28 : 44 }

        func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
            switch items[row] {
            case .header(let title):
                let label = NSTextField(labelWithString: title)
                label.font = .preferredFont(forTextStyle: .headline)
                return label
            case .entry(let entry):
                let dot = NSTextField(labelWithString: entry.unread ? "●" : " ")
                dot.textColor = .controlAccentColor
                dot.setAccessibilityLabel(entry.unread ? "Unread" : "Seen")
                let title = NSTextField(labelWithString: entry.title)
                title.lineBreakMode = .byTruncatingTail
                title.maximumNumberOfLines = 2
                let subtitle = NSTextField(labelWithString: entry.subtitle)
                subtitle.lineBreakMode = .byTruncatingTail
                subtitle.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
                subtitle.textColor = .secondaryLabelColor
                let text = NSStackView(views: [title, subtitle])
                text.orientation = .vertical
                text.alignment = .leading
                text.spacing = 1
                let stack = NSStackView(views: [dot, text])
                stack.orientation = .horizontal
                stack.alignment = .centerY
                stack.spacing = 6
                stack.edgeInsets = NSEdgeInsets(top: 4, left: 6, bottom: 4, right: 6)
                return stack
            }
        }

        func tableViewSelectionDidChange(_ notification: Notification) {
            guard !updating, let table = notification.object as? NSTableView else { return }
            let row = table.selectedRow
            let next = items.indices.contains(row) ? items[row].entryID : nil
            if parent.selection != next { parent.selection = next }
        }
    }
}
