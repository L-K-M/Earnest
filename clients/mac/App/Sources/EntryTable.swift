import AppKit
import SwiftUI

struct EntryRow: Identifiable, Equatable {
    let id: Int64
    let title: String
}

struct EntryTable: NSViewRepresentable {
    let entries: [EntryRow]
    @Binding var selection: Int64?

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let table = NSTableView()
        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("title"))
        column.title = "Articles"
        table.addTableColumn(column)
        table.headerView = nil
        table.delegate = context.coordinator
        table.dataSource = context.coordinator
        table.rowHeight = 28
        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        guard let table = scroll.documentView as? NSTableView else { return }
        context.coordinator.parent = self
        table.reloadData()
        if let selection, let row = entries.firstIndex(where: { $0.id == selection }) {
            table.selectRowIndexes(IndexSet(integer: row), byExtendingSelection: false)
        } else {
            table.deselectAll(nil)
        }
    }

    final class Coordinator: NSObject, NSTableViewDataSource, NSTableViewDelegate {
        var parent: EntryTable
        init(_ parent: EntryTable) { self.parent = parent }
        func numberOfRows(in tableView: NSTableView) -> Int { parent.entries.count }
        func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
            let cell = NSTextField(labelWithString: parent.entries[row].title)
            cell.lineBreakMode = .byTruncatingTail
            return cell
        }
        func tableViewSelectionDidChange(_ notification: Notification) {
            guard let table = notification.object as? NSTableView else { return }
            let row = table.selectedRow
            let next = parent.entries.indices.contains(row) ? parent.entries[row].id : nil
            if parent.selection != next { parent.selection = next }
        }
    }
}
