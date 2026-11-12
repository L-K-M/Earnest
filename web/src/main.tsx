import {createRoot} from 'react-dom/client'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {App} from './App'
import {registerOffline} from './offline/Offline'
import './style.css'
import './front-layout.css'

void registerOffline().catch(error => console.error('Offline reading unavailable:', error))
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:1,networkMode:'always'}}})}><App /></QueryClientProvider>)
