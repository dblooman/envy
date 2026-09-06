import React, { useEffect, useState } from 'react'

export function App() {
  const graphqlURL = import.meta.env.VITE_GRAPHQL_URL || 'http://localhost:4000/graphql'
  const compositionID = import.meta.env.ENVY_COMPOSITION_ID || 'local'
  const [healthStatus, setHealthStatus] = useState<'checking' | 'healthy' | 'unreachable'>('checking')

  useEffect(() => {
    // Probe the backend composition GraphQL health
    fetch(graphqlURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: '{ __typename }' }),
    })
      .then((res) => {
        if (res.ok) setHealthStatus('healthy')
        else setHealthStatus('unreachable')
      })
      .catch(() => setHealthStatus('unreachable'))
  }, [graphqlURL])

  return (
    <div style={{ fontFamily: 'system-ui, sans-serif', padding: '2rem', maxWidth: 800, margin: '0 auto' }}>
      <h1>Envy Cloudflare Pages Preview Example</h1>
      <p>This frontend was built with the Envy build adapter, bound to backend composition:</p>
      <div style={{ background: '#f4f4f5', padding: '1rem', borderRadius: 8, fontFamily: 'monospace' }}>
        <div><strong>Composition ID:</strong> {compositionID}</div>
        <div><strong>GraphQL URL:</strong> {graphqlURL}</div>
        <div><strong>Backend Status:</strong> {healthStatus}</div>
      </div>
    </div>
  )
}
export default App
