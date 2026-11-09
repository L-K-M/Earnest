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

    var row: EntryRow? {
        if case .entry(let row) = self { return row }
        return nil
    }
}

/// What a row's context menu and double-click can do. Actions name the row
/// they came from, which need not be the selected one.
struct EntryTableActions {
    var perform: (EntryCommand, Int64) -> Void = { _, _ in }
    var openInBrowser: (Int64) -> Void = { _ in }
    var canAct: Bool = false
    /// Whether reading actions apply to a particular row.
    var canActOn: (Int64) -> Bool = { _ in true }
}

struct EntryTable: NSViewRepresentable {
    let entries: [EntryRow]
    let groups: [EditionGroup]
    @Binding var selection: Int64?
    var actions = EntryTableActions()
    /// Returns true once when the window wants the list to take keyboard focus.
    var takeFocusRequest: () -> Bool = { false }

    init(entries: [EntryRow] = [], groups: [EditionGroup] = [], selection: Binding<Int64?>,
         actions: EntryTableActions = EntryTableActions(), takeFocusRequest: @escaping () -> Bool = { false }) {
        self.entries = entries
        self.groups = groups
        _selection = selection
        self.actions = actions
        self.takeFocusRequest = takeFocusRequest
    }

    private var items: [TableItem] {
        if groups.isEmpty { return entries.map(TableItem.entry) }
        return groups.flatMap { [.header($0.title)] + $0.rows.map(TableItem.entry) }
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let table = EntryTableView()
        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("title"))
        column.title = "Articles"
        column.resizingMask = .autoresizingMask
        table.addTableColumn(column)
        table.columnAutoresizingStyle = .uniformColumnAutoresizingStyle
        table.headerView = nil
        table.style = .inset
        table.allowsEmptySelection = true
        table.allowsMultipleSelection = false
        table.floatsGroupRows = true
        table.delegate = context.coordinator
        table.dataSource = context.coordinator
        table.target = context.coordinator
        table.doubleAction = #selector(Coordinator.doubleClicked(_:))
        table.setDraggingSourceOperationMask(.copy, forLocal: false)
        // Article links dragged inside Earnest are not subscription offers.
        table.setDraggingSourceOperationMask([], forLocal: true)
        table.setAccessibilityLabel("Articles")
        let menu = NSMenu()
        menu.delegate = context.coordinator
        // Items carry their own enabled state for the clicked row.
        menu.autoenablesItems = false
        table.menu = menu
        table.coordinator = context.coordinator
        context.coordinator.table = table
        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.contentView.postsFrameChangedNotifications = true
        context.coordinator.observeWidth(of: scroll.contentView, table: table)
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
            coordinator.heights.removeAll()
            table.reloadData()
        }
        if let selection, let row = coordinator.items.firstIndex(where: { $0.entryID == selection }) {
            if table.selectedRow != row {
                table.selectRowIndexes(IndexSet(integer: row), byExtendingSelection: false)
            }
            // Scroll only when the selection itself changes, so an unrelated
            // update never yanks a list you are scrolling back to the article.
            if coordinator.revealed != selection {
                coordinator.revealed = selection
                table.scrollRowToVisible(row)
            }
        } else {
            coordinator.revealed = nil
            if table.selectedRow >= 0 { table.deselectAll(nil) }
        }
        // A new window starts with the list focused so j, k and the arrow
        // keys work at once.
        if !coordinator.focused, let window = table.window, window.firstResponder === window || window.firstResponder == nil {
            coordinator.focused = window.makeFirstResponder(table)
        }
        if takeFocusRequest() {
            // After the search field has handed back the keyboard.
            DispatchQueue.main.async { table.window?.makeFirstResponder(table) }
        }
    }

    /// Copies the selected article's link, as Edit > Copy does in Mail's list.
    final class EntryTableView: NSTableView {
        weak var coordinator: Coordinator?

        @objc func copy(_ sender: Any?) {
            guard let coordinator, let row = coordinator.items[safe: selectedRow]?.row else { return }
            coordinator.write(row, to: .general)
        }

        override func validateUserInterfaceItem(_ item: any NSValidatedUserInterfaceItem) -> Bool {
            if item.action == #selector(copy(_:)) {
                return coordinator?.items[safe: selectedRow]?.row?.url != nil
            }
            return super.validateUserInterfaceItem(item)
        }
    }

    @MainActor final class Coordinator: NSObject, NSTableViewDataSource, NSTableViewDelegate, NSMenuDelegate {
        var parent: EntryTable
        fileprivate var items: [TableItem]
        var updating = false
        var focused = false
        /// The selection last scrolled into view.
        var revealed: Int64?
        fileprivate var heights: [Int: CGFloat] = [:]
        private var width: CGFloat = 0
        nonisolated(unsafe) private var observer: NSObjectProtocol?

        init(_ parent: EntryTable) { self.parent = parent; items = parent.items }

        deinit {
            if let observer { NotificationCenter.default.removeObserver(observer) }
        }

        func observeWidth(of view: NSView, table: NSTableView) {
            observer = NotificationCenter.default.addObserver(forName: NSView.frameDidChangeNotification, object: view, queue: .main) { [weak self, weak table] _ in
                MainActor.assumeIsolated {
                    guard let self, let table else { return }
                    let width = table.bounds.width
                    guard abs(width - self.width) > 0.5 else { return }
                    self.width = width
                    self.heights.removeAll()
                    table.noteHeightOfRows(withIndexesChanged: IndexSet(integersIn: 0..<table.numberOfRows))
                }
            }
        }

        func numberOfRows(in tableView: NSTableView) -> Int { items.count }

        func tableView(_ tableView: NSTableView, isGroupRow row: Int) -> Bool { items[row].entryID == nil }
        func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool { items[row].entryID != nil }

        func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat {
            guard let entry = items[row].row else { return 28 }
            let width = max(120, tableView.bounds.width - 32)
            if let cached = heights[row] { return cached }
            let height = EntryCell.height(for: entry, width: width)
            heights[row] = height
            return height
        }

        func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
            switch items[row] {
            case .header(let title):
                let identifier = NSUserInterfaceItemIdentifier("header")
                let label = (tableView.makeView(withIdentifier: identifier, owner: nil) as? NSTextField) ?? {
                    let label = NSTextField(labelWithString: "")
                    label.identifier = identifier
                    label.font = .preferredFont(forTextStyle: .headline)
                    label.textColor = .secondaryLabelColor
                    label.lineBreakMode = .byTruncatingTail
                    return label
                }()
                label.stringValue = title
                label.setAccessibilityRole(.staticText)
                label.setAccessibilityLabel("\(title), feed")
                return label
            case .entry(let entry):
                let identifier = NSUserInterfaceItemIdentifier("entry")
                let cell = (tableView.makeView(withIdentifier: identifier, owner: nil) as? EntryCell) ?? EntryCell(identifier: identifier)
                cell.configure(entry)
                return cell
            }
        }

        func tableViewSelectionDidChange(_ notification: Notification) {
            guard !updating, let table = notification.object as? NSTableView else { return }
            let row = table.selectedRow
            let next = items.indices.contains(row) ? items[row].entryID : nil
            if parent.selection != next { parent.selection = next }
        }

        // MARK: Double-click, drag and copy

        @objc func doubleClicked(_ sender: NSTableView) {
            guard let id = items[safe: sender.clickedRow]?.entryID else { return }
            parent.actions.openInBrowser(id)
        }

        func tableView(_ tableView: NSTableView, pasteboardWriterForRow row: Int) -> (any NSPasteboardWriting)? {
            guard let url = items[safe: row]?.row?.url else { return nil }
            return url as NSURL
        }

        func write(_ row: EntryRow, to pasteboard: NSPasteboard) {
            guard let url = row.url else { return }
            pasteboard.clearContents()
            pasteboard.writeObjects([url as NSURL])
            pasteboard.setString(url.absoluteString, forType: .string)
        }

        // MARK: Context menu

        func menuNeedsUpdate(_ menu: NSMenu) {
            menu.removeAllItems()
            guard let table, let row = items[safe: table.clickedRow]?.row else { return }
            let canAct = parent.actions.canAct && parent.actions.canActOn(row.id)
            func item(_ title: String, _ symbol: String, enabled: Bool = true, _ action: @escaping () -> Void) {
                let menuItem = ClosureMenuItem(title: title, action: action)
                menuItem.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
                menuItem.isEnabled = enabled
                menu.addItem(menuItem.showingImage())
            }
            let id = row.id
            item("Open in Browser", "safari", enabled: row.url != nil) { [weak self] in self?.parent.actions.openInBrowser(id) }
            menu.addItem(.separator())
            item("Move to Done", "checkmark.circle", enabled: canAct) { [weak self] in self?.parent.actions.perform(.done, id) }
            item(row.later ? "Remove from Later" : "Move to Later", row.later ? "clock.badge.xmark" : "clock", enabled: canAct) { [weak self] in self?.parent.actions.perform(.later, id) }
            item(row.kept ? "Unkeep" : "Keep", row.kept ? "bookmark.slash" : "bookmark", enabled: canAct) { [weak self] in self?.parent.actions.perform(.keep, id) }
            item("Mark as Unread", "circle.fill", enabled: canAct && !row.unread) { [weak self] in self?.parent.actions.perform(.unread, id) }
            menu.addItem(.separator())
            item("Copy Link", "link", enabled: row.url != nil) { [weak self] in self?.write(row, to: .general) }
            if let url = row.url {
                menu.addItem(NSSharingServicePicker(items: [url]).standardShareMenuItem.showingImage())
            }
        }

        weak var table: NSTableView?
    }
}

extension NSMenuItem {
    /// Context-menu icons help tell the reading actions apart at a glance.
    /// From macOS 27, AppKit hides menu item images unless an item asks to
    /// show them (`preferredImageVisibility`, macOS 27 and later); on macOS
    /// 26 they are shown already.
    func showingImage() -> NSMenuItem {
        if #available(macOS 27, *) { preferredImageVisibility = .visible }
        return self
    }
}

/// A menu item that runs a closure, for context menus built per click.
final class ClosureMenuItem: NSMenuItem {
    private let handler: () -> Void

    init(title: String, action handler: @escaping () -> Void) {
        self.handler = handler
        super.init(title: title, action: #selector(run), keyEquivalent: "")
        target = self
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    @objc private func run() { handler() }
}

/// One article row: an unread dot, a title of up to two lines, its source
/// line, and markers for Kept and Later. Heights are measured from the same
/// fonts so rows fit their text at any column width.
final class EntryCell: NSTableCellView {
    private let dot = NSImageView()
    private let title = NSTextField(wrappingLabelWithString: "")
    private let subtitle = NSTextField(labelWithString: "")
    private let marks = NSImageView()

    static let titleFont = NSFont.preferredFont(forTextStyle: .body)
    static let unreadTitleFont = NSFont.systemFont(ofSize: titleFont.pointSize, weight: .semibold)
    static let subtitleFont = NSFont.preferredFont(forTextStyle: .subheadline)
    private static let inset = NSEdgeInsets(top: 7, left: 18, bottom: 7, right: 6)
    private static let markWidth: CGFloat = 16

    init(identifier: NSUserInterfaceItemIdentifier) {
        super.init(frame: .zero)
        self.identifier = identifier
        dot.image = NSImage(systemSymbolName: "circle.fill", accessibilityDescription: nil)?
            .withSymbolConfiguration(.init(pointSize: 7, weight: .regular))
        dot.contentTintColor = .controlAccentColor
        title.maximumNumberOfLines = 2
        title.lineBreakMode = .byTruncatingTail
        title.cell?.truncatesLastVisibleLine = true
        subtitle.font = Self.subtitleFont
        subtitle.textColor = .secondaryLabelColor
        subtitle.lineBreakMode = .byTruncatingTail
        marks.contentTintColor = .secondaryLabelColor
        for view in [dot, title, subtitle, marks] { addSubview(view) }
        setAccessibilityElement(true)
        setAccessibilityRole(.cell)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func configure(_ row: EntryRow) {
        title.stringValue = row.title
        title.font = row.unread ? Self.unreadTitleFont : Self.titleFont
        subtitle.stringValue = row.subtitle
        dot.isHidden = !row.unread
        let symbol = row.kept ? "bookmark.fill" : row.later ? "clock" : nil
        marks.image = symbol.flatMap { NSImage(systemSymbolName: $0, accessibilityDescription: nil)?
            .withSymbolConfiguration(.init(pointSize: 10, weight: .regular)) }
        marks.isHidden = symbol == nil
        var spoken = [row.title, row.subtitle]
        if row.unread { spoken.insert("Unread", at: 0) }
        if row.kept { spoken.append("Kept") }
        if row.later { spoken.append("Later") }
        setAccessibilityLabel(spoken.joined(separator: ", "))
        toolTip = row.title
        needsLayout = true
    }

    override func layout() {
        super.layout()
        let inset = Self.inset
        let textWidth = max(40, bounds.width - inset.left - inset.right - Self.markWidth)
        let titleHeight = Self.titleHeight(title.stringValue, font: title.font ?? Self.titleFont, width: textWidth)
        let subtitleHeight = ceil(Self.subtitleFont.ascender - Self.subtitleFont.descender + Self.subtitleFont.leading)
        // Flipped-agnostic: AppKit's origin is bottom-left.
        let top = bounds.height - inset.top
        title.frame = NSRect(x: inset.left, y: top - titleHeight, width: textWidth, height: titleHeight)
        title.preferredMaxLayoutWidth = textWidth
        subtitle.frame = NSRect(x: inset.left, y: top - titleHeight - 2 - subtitleHeight,
                                width: textWidth, height: subtitleHeight)
        let lineHeight = ceil(Self.titleFont.ascender - Self.titleFont.descender)
        dot.frame = NSRect(x: 5, y: top - lineHeight / 2 - 4, width: 8, height: 8)
        marks.frame = NSRect(x: bounds.width - inset.right - Self.markWidth + 2, y: top - lineHeight / 2 - 7,
                             width: 14, height: 14)
    }

    static func height(for row: EntryRow, width: CGFloat) -> CGFloat {
        let textWidth = max(40, width - inset.left - inset.right - markWidth)
        let titleHeight = titleHeight(row.title, font: row.unread ? unreadTitleFont : titleFont, width: textWidth)
        let subtitleHeight = ceil(subtitleFont.ascender - subtitleFont.descender + subtitleFont.leading)
        return ceil(inset.top + titleHeight + 2 + subtitleHeight + inset.bottom)
    }

    private static func titleHeight(_ text: String, font: NSFont, width: CGFloat) -> CGFloat {
        let line = ceil(font.ascender - font.descender + font.leading)
        let bounds = (text as NSString).boundingRect(
            with: NSSize(width: width, height: .greatestFiniteMagnitude),
            options: [.usesLineFragmentOrigin, .usesFontLeading], attributes: [.font: font])
        return min(line * 2, max(line, ceil(bounds.height)))
    }
}

extension Array {
    subscript(safe index: Int) -> Element? { indices.contains(index) ? self[index] : nil }
}
