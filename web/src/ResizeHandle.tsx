import {useCallback, useEffect, useRef, useState, type PointerEvent} from 'react'
import {createPortal} from 'react-dom'

type Props = {
  label: string
  controls: string
  value: number
  min: number
  max: number
  onChange: (width: number) => void
  onReset: () => void
}
type Drag = {pointer: number; x: number; width: number; element: HTMLDivElement}

export function ResizeHandle({label, controls, value, min, max, onChange, onReset}: Props) {
  const drag = useRef<Drag | null>(null)
  const [resizing, setResizing] = useState(false)
  const release = useCallback(() => {
    const current = drag.current
    drag.current = null
    if (current?.element.hasPointerCapture(current.pointer)) current.element.releasePointerCapture(current.pointer)
  }, [])
  const finish = useCallback(() => {release(); setResizing(false)}, [release])
  useEffect(() => {
    if (!resizing) return
    window.addEventListener('blur', finish)
    return () => {window.removeEventListener('blur', finish); release()}
  }, [resizing, finish, release])

  function change(width: number) { onChange(Math.min(max, Math.max(min, width))) }
  function end(event: PointerEvent<HTMLDivElement>) {
    if (event.pointerId === drag.current?.pointer) finish()
  }

  return <>
    <div role="separator" aria-orientation="vertical" aria-label={label} aria-controls={controls}
      aria-valuemin={min} aria-valuemax={max} aria-valuenow={value} aria-valuetext={`${value} pixels`}
      tabIndex={0} className={`resize-handle${resizing ? ' is-resizing' : ''}`}
      title="Drag or use arrow keys to resize. Double-click or press Enter to reset."
      onPointerDown={event => {
        if (!event.isPrimary || event.button !== 0 || drag.current) return
        event.preventDefault()
        event.currentTarget.focus()
        event.currentTarget.setPointerCapture(event.pointerId)
        drag.current = {pointer: event.pointerId, x: event.clientX, width: value, element: event.currentTarget}
        setResizing(true)
      }}
      onPointerMove={event => {
        const current = drag.current
        if (event.pointerId !== current?.pointer || !current) return
        change(current.width + event.clientX - current.x)
      }}
      onPointerUp={end} onPointerCancel={end} onLostPointerCapture={end}
      onDoubleClick={onReset}
      onKeyDown={event => {
        if (event.altKey || event.ctrlKey || event.metaKey) return
        const step = event.shiftKey ? 48 : 16
        switch (event.key) {
        case 'ArrowLeft': change(value - step); break
        case 'ArrowRight': change(value + step); break
        case 'Home': change(min); break
        case 'End': change(max); break
        case 'Enter': onReset(); break
        default: return
        }
        event.preventDefault()
        event.stopPropagation()
      }} />
    {/* Pointer capture keeps events on the handle; the shield also prevents
        article iframes from stealing the cursor or starting text selection. */}
    {resizing && createPortal(<div className="resize-shield" aria-hidden="true" />, document.body)}
  </>
}
