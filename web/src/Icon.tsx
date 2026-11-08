// Inline stroke icons drawn for Earnest on a 20-unit grid. They inherit the
// text colour and are always decorative: every control that shows one also
// has a text label, visible or visually hidden.
const paths = {
  menu: 'M3 5.5h14M3 10h14M3 14.5h14',
  search: 'M8.75 14.5a5.75 5.75 0 1 0 0-11.5 5.75 5.75 0 0 0 0 11.5ZM13 13l4 4',
  command: 'M7 7h6v6H7zM7 7V5a2 2 0 1 0-2 2h2Zm6 0V5a2 2 0 1 1 2 2h-2Zm0 6v2a2 2 0 1 0 2-2h-2Zm-6 0v2a2 2 0 1 1-2-2h2Z',
  back: 'M12.5 4.5 7 10l5.5 5.5',
  close: 'M5 5l10 10M15 5 5 15',
  refresh: 'M15.5 8A6 6 0 1 0 16 11M15.5 3.5V8H11',
  earlier: 'M10 16V4M5 9l5-5 5 5',
  stack: 'M3 7.5 10 4l7 3.5-7 3.5-7-3.5ZM3 11l7 3.5 7-3.5M3 14.5 10 18l7-3.5',
  done: 'M4 10.5 8 14.5 16 5.5',
  later: 'M10 17a7 7 0 1 0 0-14 7 7 0 0 0 0 14ZM10 6v4l2.5 2.5',
  keep: 'M6 3.5h8v13l-4-3-4 3v-13Z',
  unread: 'M10 14a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z',
  external: 'M11 4h5v5M16 4l-7 7M14 11.5V16H4V6h4.5',
  link: 'M8.5 11.5a3 3 0 0 0 4.24 0l2.5-2.5a3 3 0 0 0-4.24-4.24L10 5.76M11.5 8.5a3 3 0 0 0-4.24 0l-2.5 2.5a3 3 0 0 0 4.24 4.24L10 14.24',
  share: 'M10 3v9M6.5 6.5 10 3l3.5 3.5M5 10H4v7h12v-7h-1',
  type: 'M3 15.5 7 4.5l4 11M4.5 11.5h5M12.5 9.5h4M14.5 9.5v6M13 15.5h3',
  chevron: 'M7.5 5 12.5 10l-5 5',
  more: 'M5 10h.01M10 10h.01M15 10h.01',
  settings: 'M10 12.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5ZM10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M4.7 15.3l1.4-1.4M13.9 6.1l1.4-1.4',
  warning: 'M10 3.5 17.5 16.5h-15L10 3.5ZM10 8.5v3.5M10 14.25h.01',
  check: 'M4.5 10.5 8 14l7.5-8',
  sync: 'M4 8a6 6 0 0 1 10.5-2.5M16 12a6 6 0 0 1-10.5 2.5M14.5 2.5v3h-3M5.5 17.5v-3h3',
  offline: 'M3 3l14 14M6.5 9.2A6 6 0 0 1 9 8.1M3.5 6.8a10 10 0 0 1 3-1.8M13.4 8.6a6 6 0 0 1 1.7 1.2M16.5 6.8a10 10 0 0 0-5.3-2.3M8 12.5a3 3 0 0 1 3.4-.5M10 15.5h.01',
  undo: 'M7.5 4.5 4 8l3.5 3.5M4 8h7.5a4.5 4.5 0 0 1 0 9H9',
  today: 'M10 13.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7ZM10 2v1.5M10 16.5V18M2 10h1.5M16.5 10H18M4.3 4.3l1.1 1.1M14.6 14.6l1.1 1.1M4.3 15.7l1.1-1.1M14.6 5.4l1.1-1.1',
  edition: 'M4 4h10v11.5a1.5 1.5 0 0 0 1.5 1.5H5.5A1.5 1.5 0 0 1 4 15.5V4ZM14 7h2v8.5a1.5 1.5 0 0 1-1.5 1.5M6.5 7h5M6.5 10h5M6.5 13h3',
  ask: 'M4 4.5h12v8.5H9l-3.5 3v-3H4V4.5ZM7.5 8.75h.01M10 8.75h.01M12.5 8.75h.01',
  rules: 'M3.5 4.5h13L11.5 10.5v5l-3 1.5v-6.5L3.5 4.5Z',
  activity: 'M2.5 10h3l2-5 4 10 2-5h4',
  bell: 'M5.5 13.5V9a4.5 4.5 0 0 1 9 0v4.5l1.5 2H4l1.5-2ZM8.5 17.5h3',
  feeds: 'M4.5 4.5a11 11 0 0 1 11 11M4.5 9a6.5 6.5 0 0 1 6.5 6.5M5 15.5h.01',
  keyboard: 'M2.5 5.5h15v9h-15zM5.5 8.5h.01M8.5 8.5h.01M11.5 8.5h.01M14.5 8.5h.01M6.5 11.5h7',
  exit: 'M8 4.5H4.5v11H8M12 6.5 15.5 10 12 13.5M15.5 10H7.5',
  folder: 'M2.5 5.5a1 1 0 0 1 1-1h4l1.5 2h7.5a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1h-13a1 1 0 0 1-1-1v-10Z',
  tag: 'M3.5 3.5h6.5l7 7-6.5 6.5-7-7V3.5ZM7 7h.01',
  saved: 'M8.25 13a4.75 4.75 0 1 0 0-9.5 4.75 4.75 0 0 0 0 9.5ZM11.75 11.75 16.5 16.5M6.5 8.25h3.5',
} as const

export type IconName = keyof typeof paths

export function Icon({name, size = 18}: {name: IconName; size?: number}) {
  return <svg className="icon" width={size} height={size} viewBox="0 0 20 20" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d={paths[name]} /></svg>
}
