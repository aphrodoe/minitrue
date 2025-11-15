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
    setQueryResult(queryData);
    setLoading(false);
  };

  return (
    <div className="App">
      <QueryForm onSubmit={handleQuery} loading={loading} />
      {error && <div className="error-message"><strong>Error:</strong> {error}</div>}
      {queryResult && <QueryResults result={queryResult} />}
    </div>
  );
}

export default App;
