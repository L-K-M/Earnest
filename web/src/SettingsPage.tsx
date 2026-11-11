import {useEffect} from 'react'
import {Settings, type ReadingSettings} from './Settings'
import {LabelSettings} from './labels/LabelSettings'
import {Push, Tokens, Webhooks} from './account/Account'
import {LLMSettings} from './llm/LLMSettings'
import {Link, whenPresent} from './router'

const SECTIONS = [
  {id:'reading', title:'Reading'},
  {id:'labels', title:'Labels'},
  {id:'tokens', title:'Tokens and devices'},
  {id:'push', title:'Push'},
  {id:'webhooks', title:'Webhooks'},
  {id:'llm', title:'LLM services'},
] as const

// Settings is one page with a section index, so every part has its own
// address (/settings#push) and a device approval link (/device) lands on the
// code field ready to confirm.
export function SettingsPage({device, settings, onSaved}: {device: boolean; settings: ReadingSettings; onSaved: (settings: ReadingSettings) => void}) {
  useEffect(() => {
    if (!device) return
    return whenPresent(() => document.getElementById('device-code'), field => {
      field.scrollIntoView({block: 'center'})
      field.focus({preventScroll: true})
    })
  }, [device])
  return <div className="settings-layout">
    <div className="page-header"><h2>{device ? 'Approve a device' : 'Settings'}</h2></div>
    <nav className="section-nav" aria-label="Settings sections"><ul className="section-list">{SECTIONS.map(section => <li key={section.id}><Link to={`/settings#${section.id}`} replace>{section.title}</Link></li>)}</ul></nav>
    <div className="settings-sections">
      <div id="reading" className="card"><Settings settings={settings} onSaved={onSaved} /></div>
      <div id="labels" className="card"><LabelSettings /></div>
      <div id="tokens" className="card"><Tokens /></div>
      <div id="push" className="card"><Push /></div>
      <div id="webhooks" className="card"><Webhooks /></div>
      <div id="llm" className="card"><LLMSettings /></div>
    </div>
  </div>
}
