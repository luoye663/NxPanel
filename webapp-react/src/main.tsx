import React from 'react'
import ReactDOM from 'react-dom/client'
import '@mantine/core/styles.css'
import '@mantine/dates/styles.css'
import '@mantine/notifications/styles.css'
import 'mantine-react-table/styles.css'
import './styles/global.css'
import App from './App'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
