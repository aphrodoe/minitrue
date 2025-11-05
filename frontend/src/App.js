import React, { useState } from 'react';
import './App.css';
import QueryForm from './components/QueryForm';
import QueryResults from './components/QueryResults';

function App() {
  const [queryResult, setQueryResult] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  const handleQuery = async (queryData) => {
    setLoading(true);
    setError(null);
    setQueryResult(null);

    try {
      const response = await fetch('/query', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(queryData),
      });

      if (!response.ok) {
        const errorData = await response.text();
        throw new Error(errorData || 'Query failed');
      }

      const data = await response.json();
      setQueryResult(data);
    } catch (err) {
      setError(err.message || 'Failed to execute query');
      console.error('Query error:', err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="App">
      <header className="App-header">
        <h1>🚀 Minitrue</h1>
        <p>Decentralized Time-Series Database Query Interface</p>
      </header>

      <main className="App-main">
        <QueryForm onSubmit={handleQuery} loading={loading} />
        
        {error && (
          <div className="error-message">
            <strong>Error:</strong> {error}
          </div>
        )}

        {queryResult && <QueryResults result={queryResult} />}
      </main>

      <footer className="App-footer">
        <p>Built with React • Connected to Go Backend API</p>
      </footer>
    </div>
  );
}

export default App;

