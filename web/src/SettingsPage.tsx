import {useEffect, type ReactNode} from 'react'
import {Settings, type ReadingSettings} from './Settings'
import {LabelSettings} from './labels/LabelSettings'
import {Push, Tokens, Webhooks} from './account/Account'
import {Integrations} from './integrations/Integrations'
import {LLMSettings} from './llm/LLMSettings'
import {ImportExportSettings} from './ImportExportSettings'
import {Link, whenPresent, useLocation} from './router'

const SECTIONS = [
  {id:'reading', title:'Reading & appearance'},
  {id:'llm', title:'AI'},
  {id:'push', title:'Notifications'},
  {id:'labels', title:'Organization'},
  {id:'webhooks', title:'Connections'},
  {id:'tokens', title:'Account & devices'},
  {id:'transfer', title:'Import & export'},
] as const

// Settings is one page with a section index, so every part has its own
// address (/settings#push) and a device approval link (/device) lands on the
// code field ready to confirm.
export function SettingsPage({device, settings, onSaved}: {device: boolean; settings: ReadingSettings; onSaved: (settings: ReadingSettings) => void}) {
  const location = useLocation()
  const panes: Record<string, ReactNode> = {
    reading: <Settings settings={settings} onSaved={onSaved} />,
    labels: <LabelSettings />,
    tokens: <Tokens />,
    push: <Push />,
    webhooks: <><Webhooks /><Integrations /></>,
    llm: <LLMSettings />,
    transfer: <ImportExportSettings />,
  }
  // One dedicated pane at a time, addressed by the existing hash deep links.
  // The device approval link lands on the tokens pane with its code field.
  // #integrations is the old link to the merged Connections pane.
  const hash = location.hash.slice(1)
  const requested = device ? 'tokens' : (hash === 'integrations' ? 'webhooks' : hash)
  const active = Object.hasOwn(panes, requested) ? requested : 'reading'
  useEffect(() => {
    if (!device) return
    return whenPresent(() => document.getElementById('device-code'), field => {
      field.scrollIntoView?.({block: 'center'})
      field.focus({preventScroll: true})
    })
  }, [device])
  return <div className="settings-layout">
    <div className="page-header"><h2>{device ? 'Approve a device' : 'Settings'}</h2></div>
    <nav className="section-nav" aria-label="Settings sections"><ul className="section-list">{SECTIONS.map(section => <li key={section.id}><Link to={`/settings#${section.id}`} replace aria-current={section.id === active ? 'page' : undefined}>{section.title}</Link></li>)}</ul></nav>
    <div className="settings-sections">
      <div id={active} className="card">{panes[active]}</div>
    </div>
  </div>
}
